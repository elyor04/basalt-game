package game

import (
	"context"
	crand "crypto/rand"
	"encoding/hex"
	mrand "math/rand/v2"
	"net"
	"slices"
	"strconv"
	"sync"
	"time"

	"basalt/internal/engine"
	"basalt/internal/lan"
)

// Local-network play: one device hosts, the other joins with the host's code
// (or picks it from the list of games found on the network). The devices may
// be on Wi-Fi, Ethernet or one of each, as long as they share a network.
//
// The host is the referee. The guest sends what it wants to do (move, resign,
// rematch); the host checks it with the same rules engine and answers with
// the whole move list ("sync"). The rules are deterministic, so the guest
// replays that list and reaches the identical position; a guest move the
// host did not accept is simply replaced by the host's list.
//
// A dropped connection is retried for lanGrace. A game that cannot be resumed
// in time is abandoned and counts for nobody; leaving a game that has started
// counts as resigning it.

// Errors for local-network play.
var (
	ErrLANBadCode     = &GameError{"lan_bad_code", "that code is not valid"}
	ErrLANUnreachable = &GameError{"lan_unreachable", "no game answered at that code"}
	ErrLANBusy        = &GameError{"lan_busy", "that game already has two players"}
	ErrLANVersion     = &GameError{"lan_version", "the other device runs a different version of Basalt"}
	ErrLANNoNetwork   = &GameError{"lan_no_network", "this device is not on a local network"}
	ErrLANHost        = &GameError{"lan_host_failed", "could not open a game on this network"}
	ErrLANOffline     = &GameError{"lan_offline", "the connection to your rival is down"}
	ErrNoHints        = &GameError{"no_hints", "hints are off in network games"}
	errProtocol       = &GameError{"lan_protocol", "the other device sent something unexpected"}
)

// Phases of a network match, as reported to the UI.
const (
	PhaseOff     = "off"     // no match
	PhaseHosting = "hosting" // host: waiting for someone to join
	PhaseJoining = "joining" // guest: connecting
	PhasePlaying = "playing" // connected
	PhaseLost    = "lost"    // connection dropped; trying to resume
	PhaseClosed  = "closed"  // over: Reason says why
)

const (
	lanGrace         = 60 * time.Second
	redialEvery      = 1500 * time.Millisecond
	dialTimeout      = 3 * time.Second
	handshakeTimeout = 5 * time.Second
	netWaitLimit     = 20 * time.Second
	maxAltCodes      = 3
)

// NetStatus is everything the UI needs about the network match.
type NetStatus struct {
	Version      uint64   `json:"version"`
	Phase        string   `json:"phase"`
	Role         string   `json:"role"`   // "host" or "guest"
	Reason       string   `json:"reason"` // closed: "left", "timeout", "error", "busy", "version"
	Code         string   `json:"code"`   // the host's code, "ABCD-EFGH"
	AltCodes     []string `json:"altCodes"`
	Discoverable bool     `json:"discoverable"` // host: answering "who is hosting?" probes
	WantRematch  bool     `json:"wantRematch"`
	PeerRematch  bool     `json:"peerRematch"`
	// Game is the current network game (nil until someone has joined).
	Game *GameView `json:"game"`
	// Remote is the rival's latest move in this game, for animation.
	Remote *MoveResult `json:"remote"`
}

// LANGame is a hosted game found on the local network.
type LANGame struct {
	Code string `json:"code"`
	Name string `json:"name"`
}

type netMatch struct {
	role, phase, reason string
	code                string
	altCodes            []string
	discoverable        bool
	token               string // proves a reconnecting guest is the one who joined
	gameNo              int
	guestColor          int8
	sessID              int64
	wantMe, wantPeer    bool
	remote              *MoveResult

	ln    net.Listener    // host
	adv   *lan.Advertiser // host, while waiting for someone to join
	port  int             // host: the TCP port in the code
	name  string          // host: shown next to the game in other devices' lists
	addrs []string        // guest: where the host can be reached
	peer  *lan.Peer
	lost  int // bumps on every drop, so stale timers and redials stop

	done     chan struct{}
	stopOnce sync.Once
}

func (m *netMatch) myColor() int8 {
	if m.role == "host" {
		return engine.Other(m.guestColor)
	}
	return m.guestColor
}

func (m *netMatch) peerColor() int8 { return engine.Other(m.myColor()) }

// advertise (host) starts answering "who is hosting?" probes. Without the
// discovery port (another app holds it) the game still works by its code.
func (m *netMatch) advertise() {
	if m.adv != nil {
		return
	}
	if adv, err := lan.Advertise(m.code, m.name, m.port); err == nil {
		m.adv = adv
		m.discoverable = true
	}
}

func (m *netMatch) unadvertise() {
	m.adv.Close()
	m.adv = nil
	m.discoverable = false
}

func (m *netMatch) stop() {
	m.stopOnce.Do(func() {
		close(m.done)
		if m.ln != nil {
			_ = m.ln.Close()
		}
		m.adv.Close()
		if m.peer != nil {
			m.peer.Close()
		}
	})
}

// ---------------------------------------------------------------- bound API

// HostLAN opens a game on the local network and returns its code. Calling it
// again while hosting returns the same game.
func (g *GameService) HostLAN() (NetStatus, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.restoreLocked()
	if m := g.m; m != nil && m.role == "host" && m.phase != PhaseClosed {
		return g.netStatusLocked(), nil
	}
	g.leaveLocked()

	addrs := g.localAddrs()
	if len(addrs) == 0 {
		return NetStatus{}, ErrLANNoNetwork
	}
	var ln net.Listener
	slot := 0
	for ; slot < lan.PortSlots; slot++ {
		if l, err := net.Listen("tcp4", ":"+strconv.Itoa(lan.BasePort+slot)); err == nil {
			ln = l
			break
		}
	}
	if ln == nil {
		return NetStatus{}, ErrLANHost
	}
	m := &netMatch{role: "host", phase: PhaseHosting, ln: ln, port: lan.BasePort + slot, name: lan.DeviceName(), done: make(chan struct{})}
	for _, a := range addrs {
		c, err := lan.Encode(a.IP, slot)
		if err != nil {
			continue
		}
		if m.code == "" {
			m.code = c
		} else if len(m.altCodes) < maxAltCodes {
			m.altCodes = append(m.altCodes, c)
		}
	}
	m.advertise()
	g.m = m
	go g.acceptLoop(m)
	g.bumpLocked()
	return g.netStatusLocked(), nil
}

// BrowseLAN starts (or keeps) looking for hosted games and returns those
// seen in the last few seconds. Poll it while the join screen is open.
func (g *GameService) BrowseLAN() ([]LANGame, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.browser == nil || g.browser.Closed() {
		b, err := lan.Browse()
		if err != nil {
			return []LANGame{}, ErrLANNoNetwork
		}
		g.browser = b
	}
	out := []LANGame{}
	for _, f := range g.browser.Games() {
		// Don't list the game this device is hosting.
		if g.m != nil && g.m.role == "host" && lan.Normalize(g.m.code) == f.Code {
			continue
		}
		out = append(out, LANGame{Code: lan.Format(f.Code), Name: f.Name})
	}
	return out, nil
}

// StopBrowseLAN stops looking for games.
func (g *GameService) StopBrowseLAN() {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.browser.Close()
	g.browser = nil
}

// JoinLAN connects to the game with this code. It blocks while connecting
// (a few seconds at most); LeaveLAN cancels it.
func (g *GameService) JoinLAN(code string) (NetStatus, error) {
	ip, slot, err := lan.Decode(code)
	if err != nil {
		return NetStatus{}, ErrLANBadCode
	}
	norm := lan.Normalize(code)

	g.mu.Lock()
	g.restoreLocked()
	g.leaveLocked()
	var addrs []string
	if g.browser != nil {
		// The address the game answered from is the one that works, even
		// when the host has several networks (Wi-Fi and Ethernet).
		if f, ok := g.browser.Lookup(norm); ok {
			addrs = append(addrs, f.Addr)
		}
	}
	if a := net.JoinHostPort(ip.String(), strconv.Itoa(lan.BasePort+slot)); !slices.Contains(addrs, a) {
		addrs = append(addrs, a)
	}
	m := &netMatch{role: "guest", phase: PhaseJoining, code: lan.Format(norm), addrs: addrs, done: make(chan struct{})}
	g.m = m
	g.bumpLocked()
	g.mu.Unlock()

	p, msg, herr := dialHandshake(m.done, addrs, "")

	g.mu.Lock()
	defer g.mu.Unlock()
	if g.m != m {
		// Cancelled while connecting: say goodbye, so the host goes back to
		// waiting for someone else instead of for this device.
		if p != nil {
			p.Finish(&lan.Msg{T: "leave"})
		}
		return NetStatus{}, ErrCancelled
	}
	if herr == nil {
		m.token = msg.Token
		m.peer = p
		m.phase = PhasePlaying
		if serr := g.applySyncLocked(m, msg); serr != nil {
			herr = ErrLANUnreachable
		}
	}
	if herr != nil {
		g.m = nil
		m.stop()
		g.bumpLocked()
		return NetStatus{}, herr
	}
	g.browser.Close()
	g.browser = nil
	go g.readLoop(m, p)
	g.bumpLocked()
	return g.netStatusLocked(), nil
}

// LANStatus returns the current network state.
func (g *GameService) LANStatus() NetStatus {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.netStatusLocked()
}

// NetWait blocks until the network state is newer than since (or about 20 s
// pass) and returns it. The UI keeps one of these calls open during a match.
func (g *GameService) NetWait(ctx context.Context, since uint64) (NetStatus, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	timer := time.NewTimer(netWaitLimit)
	defer timer.Stop()
	for {
		g.mu.Lock()
		if g.ver != since {
			st := g.netStatusLocked()
			g.mu.Unlock()
			return st, nil
		}
		ch := g.verCh
		g.mu.Unlock()
		select {
		case <-ch:
		case <-ctx.Done():
			// Not an error: the Wails runtime reports a rejection that
			// arrives after JS cancelled the call as unhandled, and nobody
			// is waiting for this answer anyway.
			return g.LANStatus(), nil
		case <-timer.C:
			return g.LANStatus(), nil
		}
	}
}

// RematchLAN says whether you want another game once this one is over. The
// next game starts when both players want it; the colours swap.
func (g *GameService) RematchLAN(want bool) (NetStatus, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	m := g.m
	s := g.lanSessionLocked(m)
	if s == nil {
		return NetStatus{}, ErrNoGame
	}
	if m.phase != PhasePlaying || m.peer == nil {
		return NetStatus{}, ErrLANOffline
	}
	if !s.over() {
		return NetStatus{}, ErrBadOption
	}
	m.wantMe = want
	if m.role == "host" {
		g.maybeRematchLocked(m)
		g.sendSyncLocked(m)
	} else {
		m.peer.Send(lan.Msg{T: "rematch", Game: m.gameNo, Want: want})
	}
	g.bumpLocked()
	return g.netStatusLocked(), nil
}

// LeaveLAN ends the match. Leaving a game that has started counts as
// resigning it.
func (g *GameService) LeaveLAN() NetStatus {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.leaveLocked()
	return g.netStatusLocked()
}

// ServiceShutdown is called by Wails when the app quits.
func (g *GameService) ServiceShutdown() error {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.leaveLocked()
	g.browser.Close()
	g.browser = nil
	return nil
}

// ---------------------------------------------------------------- internals

func (g *GameService) bumpLocked() {
	g.ver++
	close(g.verCh)
	g.verCh = make(chan struct{})
}

func (g *GameService) netStatusLocked() NetStatus {
	st := NetStatus{Version: g.ver, Phase: PhaseOff, AltCodes: []string{}}
	m := g.m
	if m == nil {
		return st
	}
	st.Phase, st.Role, st.Reason = m.phase, m.role, m.reason
	st.Code = m.code
	st.AltCodes = append(st.AltCodes, m.altCodes...)
	st.Discoverable = m.discoverable && m.phase == PhaseHosting
	st.WantRematch, st.PeerRematch = m.wantMe, m.wantPeer
	if s := g.lanSessionLocked(m); s != nil {
		v := s.view()
		st.Game = &v
		if m.remote != nil && m.remote.Game.ID == s.id {
			r := *m.remote
			st.Remote = &r
		}
	}
	return st
}

// lanSessionLocked is the current game of match m, if it is still live.
func (g *GameService) lanSessionLocked(m *netMatch) *session {
	if m == nil || g.sess == nil || g.sess.mode != ModeLAN || g.sess.id != m.sessID {
		return nil
	}
	return g.sess
}

// lanConnectedLocked returns the match a network game belongs to, if the
// rival is connected right now (moves and resignations need that).
func (g *GameService) lanConnectedLocked(s *session) (*netMatch, error) {
	m := g.m
	if m == nil || m.sessID != s.id || m.phase != PhasePlaying || m.peer == nil {
		return nil, ErrLANOffline
	}
	return m, nil
}

// lanTellLocked tells the rival about something done on this device: the
// guest sends msg, the host sends its full state.
func (g *GameService) lanTellLocked(m *netMatch, msg lan.Msg) {
	if m.role == "host" {
		g.sendSyncLocked(m)
	} else if m.peer != nil {
		msg.Game = m.gameNo
		m.peer.Send(msg)
	}
	g.bumpLocked()
}

// startLANGameLocked begins game m.gameNo with these moves already played.
func (g *GameService) startLANGameLocked(m *netMatch, moves []int, resigned int8) error {
	s := &session{mode: ModeLAN, human: m.myColor(), seed: uint64(g.now().UnixNano()) & (1<<53 - 1)}
	s.start = engine.NewState()
	if err := s.replay(moves); err != nil {
		return err
	}
	if resigned == engine.Lava || resigned == engine.Ash {
		s.resigned = resigned
	}
	// Replacing an AI game you have moved in forfeits it, as NewGame does.
	if old := g.sess; old != nil && old.forfeitOnReplace() {
		old.resigned = old.human
		_, _ = g.recordLocked(old)
	}
	g.nextID++
	s.id = g.nextID
	m.sessID = s.id
	m.remote = nil
	g.sess = s
	_, _ = g.store.Update(func(p *Profile) error {
		p.Saved = nil // network games cannot be resumed after a restart
		return nil
	})
	if s.over() {
		g.recordQuietLocked(s)
	}
	return nil
}

// recordQuietLocked records a finished game, flagging a failed write on the
// view instead of returning it (the game is over either way).
func (g *GameService) recordQuietLocked(s *session) {
	if _, err := g.recordLocked(s); err != nil {
		s.saveFailed = true
	}
}

func (g *GameService) hostSyncLocked(m *netMatch) lan.Msg {
	msg := lan.Msg{T: "sync", Game: m.gameNo, Color: m.guestColor, WantHost: m.wantMe, WantGuest: m.wantPeer}
	if s := g.lanSessionLocked(m); s != nil {
		msg.Moves = append([]int{}, s.moves...)
		msg.Resigned = s.resigned
	}
	return msg
}

func (g *GameService) sendSyncLocked(m *netMatch) {
	if m.peer != nil {
		m.peer.Send(g.hostSyncLocked(m))
	}
}

// applySyncLocked brings the guest's game in line with the host's.
func (g *GameService) applySyncLocked(m *netMatch, msg lan.Msg) error {
	if msg.Game <= 0 || (msg.Color != engine.Lava && msg.Color != engine.Ash) || len(msg.Moves) > engine.NumCells*4 {
		return errProtocol
	}
	m.wantPeer, m.wantMe = msg.WantHost, msg.WantGuest
	s := g.lanSessionLocked(m)
	if s == nil || msg.Game != m.gameNo {
		m.gameNo = msg.Game
		m.guestColor = msg.Color
		if err := g.startLANGameLocked(m, msg.Moves, msg.Resigned); err != nil {
			return errProtocol
		}
		return nil
	}
	if msg.Color != m.guestColor {
		return errProtocol
	}
	switch {
	case slices.Equal(s.moves, msg.Moves):
		// An acknowledgement: nothing new.
	case !s.over() && len(msg.Moves) == len(s.moves)+1 && slices.Equal(s.moves, msg.Moves[:len(s.moves)]):
		// The rival's move. (If this side's game is already over, say it
		// resigned while that move was on its way, the default branch
		// adopts the host's list instead.)
		res, err := g.applyLocked(s, msg.Moves[len(s.moves)])
		if err != nil {
			return errProtocol
		}
		m.remote = &res
	default:
		if err := s.replay(msg.Moves); err != nil {
			return errProtocol
		}
		m.remote = nil
	}
	if (msg.Resigned == engine.Lava || msg.Resigned == engine.Ash) && s.resigned == 0 && !s.over() {
		s.resigned = msg.Resigned
	}
	if s.over() && !s.recorded {
		g.recordQuietLocked(s)
	}
	return nil
}

// maybeRematchLocked (host) starts the next game once both players want it.
func (g *GameService) maybeRematchLocked(m *netMatch) {
	if !m.wantMe || !m.wantPeer {
		return
	}
	m.gameNo++
	m.guestColor = engine.Other(m.guestColor)
	m.wantMe, m.wantPeer = false, false
	_ = g.startLANGameLocked(m, nil, 0)
}

// leaveLocked ends the match from this side.
func (g *GameService) leaveLocked() {
	m := g.m
	if m == nil {
		return
	}
	s := g.lanSessionLocked(m)
	// Only while connected: if the link is down, the rival cannot hear the
	// resignation and will record nothing, so this side records nothing too.
	if s != nil && !s.over() && len(s.moves) > 0 && m.phase == PhasePlaying {
		s.resigned = s.human
		g.recordQuietLocked(s)
	}
	if m.peer != nil {
		if m.role == "host" {
			m.peer.Send(g.hostSyncLocked(m)) // carries the resignation
		}
		m.peer.Finish(&lan.Msg{T: "leave"})
		m.peer = nil
	}
	if s != nil {
		g.sess = nil
	}
	m.stop()
	g.m = nil
	g.bumpLocked()
}

// closeLocked ends the match because of the rival or the network. The
// match stays visible (phase "closed") until this side leaves it.
func (g *GameService) closeLocked(m *netMatch, reason string) {
	if m.phase == PhaseClosed {
		return
	}
	m.phase = PhaseClosed
	m.reason = reason
	m.wantMe, m.wantPeer = false, false
	if m.peer != nil {
		m.peer.Close()
		m.peer = nil
	}
	m.stop()
	g.bumpLocked()
}

// unstartedLocked: the current game has no moves yet, so nothing is lost by
// dropping it.
func (g *GameService) unstartedLocked(m *netMatch) bool {
	s := g.lanSessionLocked(m)
	return s == nil || (len(s.moves) == 0 && !s.over())
}

// reopenLocked (host) goes back to waiting for a rival: the guest left or
// vanished before anyone moved, and the code stays valid for someone else.
func (g *GameService) reopenLocked(m *netMatch) {
	if m.peer != nil {
		m.peer.Close()
		m.peer = nil
	}
	if g.lanSessionLocked(m) != nil {
		g.sess = nil
	}
	m.sessID = 0
	m.token = ""
	m.remote = nil
	m.wantMe, m.wantPeer = false, false
	m.lost++ // stop any grace timer
	m.phase = PhaseHosting
	m.advertise()
	g.bumpLocked()
}

// peerLeftLocked: the rival left on purpose.
func (g *GameService) peerLeftLocked(m *netMatch) {
	if m.role == "host" && g.unstartedLocked(m) {
		g.reopenLocked(m)
		return
	}
	if s := g.lanSessionLocked(m); s != nil && !s.over() && len(s.moves) > 0 {
		s.resigned = m.peerColor()
		g.recordQuietLocked(s)
	}
	g.closeLocked(m, "left")
}

// connLostLocked: the connection dropped without a goodbye.
func (g *GameService) connLostLocked(m *netMatch) {
	if m.peer != nil {
		m.peer.Close()
		m.peer = nil
	}
	if m.phase == PhaseClosed {
		return
	}
	if m.role == "host" && g.unstartedLocked(m) {
		// Nothing to resume. If the same guest comes back it simply joins
		// again; meanwhile anyone else may take the seat.
		g.reopenLocked(m)
		return
	}
	m.phase = PhaseLost
	m.wantMe, m.wantPeer = false, false
	m.lost++
	seq := m.lost
	g.bumpLocked()
	go func() {
		select {
		case <-m.done:
			return
		case <-time.After(g.lanGrace):
		}
		g.mu.Lock()
		defer g.mu.Unlock()
		if g.m == m && m.phase == PhaseLost && m.lost == seq {
			g.closeLocked(m, "timeout")
		}
	}()
	if m.role == "guest" {
		go g.redial(m, seq)
	}
}

func (g *GameService) readLoop(m *netMatch, p *lan.Peer) {
	for {
		msg, err := p.Read()
		g.mu.Lock()
		if g.m != m || m.peer != p {
			// Whoever replaced or removed this peer closes it (a Finish
			// must be allowed to drain, so don't close it here).
			g.mu.Unlock()
			return
		}
		if err != nil {
			g.connLostLocked(m)
			g.mu.Unlock()
			return
		}
		if m.role == "host" {
			g.hostHandleLocked(m, msg)
		} else {
			g.guestHandleLocked(m, msg)
		}
		g.mu.Unlock()
	}
}

func (g *GameService) hostHandleLocked(m *netMatch, msg lan.Msg) {
	s := g.lanSessionLocked(m)
	switch msg.T {
	case "move":
		if s != nil && msg.Game == m.gameNo && msg.Ply == len(s.moves) && !s.over() && s.state().ToMove == m.guestColor {
			if res, err := g.applyLocked(s, msg.Cell); err == nil {
				m.remote = &res
			}
		}
		g.sendSyncLocked(m) // the acknowledgement, or the correction
	case "resign":
		if s != nil && msg.Game == m.gameNo && !s.over() {
			s.resigned = m.guestColor
			g.recordQuietLocked(s)
		}
		g.sendSyncLocked(m)
	case "rematch":
		if s != nil && msg.Game == m.gameNo && s.over() {
			m.wantPeer = msg.Want
			g.maybeRematchLocked(m)
		}
		g.sendSyncLocked(m)
	case "leave":
		g.peerLeftLocked(m)
		return
	default:
		return
	}
	g.bumpLocked()
}

func (g *GameService) guestHandleLocked(m *netMatch, msg lan.Msg) {
	switch msg.T {
	case "sync", "welcome":
		if err := g.applySyncLocked(m, msg); err != nil {
			g.closeLocked(m, "error")
			return
		}
	case "leave":
		g.peerLeftLocked(m)
		return
	default:
		return
	}
	g.bumpLocked()
}

// acceptLoop (host) takes connections until the match ends.
func (g *GameService) acceptLoop(m *netMatch) {
	for {
		c, err := m.ln.Accept()
		if err != nil {
			select {
			case <-m.done:
				return
			default:
			}
			time.Sleep(100 * time.Millisecond)
			continue
		}
		go g.hostHandshake(m, c)
	}
}

func (g *GameService) hostHandshake(m *netMatch, c net.Conn) {
	p := lan.NewPeer(c)
	msg, err := p.ReadWithin(handshakeTimeout)
	if err != nil || msg.T != "hello" {
		p.Close()
		return
	}
	if msg.V != lan.ProtocolVersion {
		p.Finish(&lan.Msg{T: "refuse", Reason: "version"})
		return
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	switch {
	case g.m != m || m.phase == PhaseClosed:
		p.Finish(&lan.Msg{T: "refuse", Reason: "busy"})
		return
	case m.phase == PhaseHosting:
		// A guest takes the seat: a new game, with a coin toss for colours.
		m.token = newToken()
		m.gameNo++
		m.guestColor = engine.Lava + int8(mrand.IntN(2))
		m.unadvertise()
		if err := g.startLANGameLocked(m, nil, 0); err != nil {
			p.Close()
			return
		}
	case msg.Token != m.token:
		p.Finish(&lan.Msg{T: "refuse", Reason: "busy"})
		return
	case m.peer != nil:
		// The guest is back before this side noticed it had gone (a silent
		// Wi-Fi drop takes a few seconds to time out): the token proves it
		// is the same player, so the new connection replaces the stale one.
		m.peer.Close()
		m.peer = nil
	}
	m.peer = p
	m.phase = PhasePlaying
	welcome := g.hostSyncLocked(m)
	welcome.T = "welcome"
	welcome.Token = m.token
	p.Send(welcome)
	g.bumpLocked()
	go g.readLoop(m, p)
}

// redial (guest) tries to get back to the host after a drop.
func (g *GameService) redial(m *netMatch, seq int) {
	for {
		select {
		case <-m.done:
			return
		case <-time.After(redialEvery):
		}
		g.mu.Lock()
		if g.m != m || m.phase != PhaseLost || m.lost != seq {
			g.mu.Unlock()
			return
		}
		token := m.token
		g.mu.Unlock()

		p, msg, err := dialHandshake(m.done, m.addrs, token)
		if err == ErrLANUnreachable {
			continue
		}
		g.mu.Lock()
		if g.m != m || m.phase != PhaseLost || m.lost != seq {
			g.mu.Unlock()
			if p != nil {
				p.Close()
			}
			return
		}
		switch err {
		case nil:
		case ErrLANVersion:
			g.closeLocked(m, "version")
			g.mu.Unlock()
			return
		case ErrLANBusy:
			// The host no longer knows this match (it left, or restarted).
			g.closeLocked(m, "left")
			g.mu.Unlock()
			return
		default:
			g.mu.Unlock()
			return
		}
		m.peer = p
		m.phase = PhasePlaying
		m.token = msg.Token // a host that had reopened issues a new one
		if serr := g.applySyncLocked(m, msg); serr != nil {
			g.closeLocked(m, "error")
			g.mu.Unlock()
			return
		}
		g.bumpLocked()
		g.mu.Unlock()
		go g.readLoop(m, p)
		return
	}
}

// dialHandshake connects to the first address that answers and says hello.
// It returns the welcome message, ErrLANBusy / ErrLANVersion if the host
// refused, ErrCancelled, or ErrLANUnreachable.
func dialHandshake(done <-chan struct{}, addrs []string, token string) (*lan.Peer, lan.Msg, error) {
	for _, a := range addrs {
		select {
		case <-done:
			return nil, lan.Msg{}, ErrCancelled
		default:
		}
		c, err := net.DialTimeout("tcp4", a, dialTimeout)
		if err != nil {
			continue
		}
		p := lan.NewPeer(c)
		p.Send(lan.Msg{T: "hello", V: lan.ProtocolVersion, Token: token})
		msg, err := p.ReadWithin(handshakeTimeout)
		if err != nil {
			p.Close()
			continue
		}
		switch msg.T {
		case "welcome":
			return p, msg, nil
		case "refuse":
			p.Close()
			if msg.Reason == "version" {
				return nil, msg, ErrLANVersion
			}
			return nil, msg, ErrLANBusy
		}
		p.Close()
	}
	return nil, lan.Msg{}, ErrLANUnreachable
}

func newToken() string {
	var b [12]byte
	_, _ = crand.Read(b[:])
	return hex.EncodeToString(b[:])
}

// playLANLocked places a stone for this device in a network game and tells
// the rival. The caller (Play) has checked the game is live.
func (g *GameService) playLANLocked(s *session, cell int) (MoveResult, error) {
	m, err := g.lanConnectedLocked(s)
	if err != nil {
		return MoveResult{}, err
	}
	if s.over() {
		return MoveResult{}, ErrOver
	}
	if s.state().ToMove != s.human {
		return MoveResult{}, ErrNotYourTurn
	}
	res, err := g.applyLocked(s, cell)
	if err != nil {
		return MoveResult{}, err
	}
	g.lanTellLocked(m, lan.Msg{T: "move", Ply: len(s.moves) - 1, Cell: cell})
	return res, nil
}
