package game

import (
	"context"
	"net"
	"strconv"
	"testing"
	"time"

	"basalt/internal/engine"
	"basalt/internal/lan"
)

// lanPair returns a host and a guest service that talk over loopback.
func lanPair(t *testing.T) (*GameService, *GameService) {
	t.Helper()
	host, _ := testService(t)
	guest, _ := testService(t)
	host.localAddrs = func() []lan.LocalAddr {
		return []lan.LocalAddr{{IP: net.IPv4(127, 0, 0, 1).To4(), Broadcast: net.IPv4(127, 255, 255, 255).To4()}}
	}
	t.Cleanup(func() {
		_ = host.ServiceShutdown()
		_ = guest.ServiceShutdown()
	})
	return host, guest
}

// waitNet waits until g's network status satisfies ok.
func waitNet(t *testing.T, g *GameService, what string, ok func(NetStatus) bool) NetStatus {
	t.Helper()
	deadline := time.Now().Add(12 * time.Second)
	st := g.LANStatus()
	for !ok(st) {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s; phase %q reason %q", what, st.Phase, st.Reason)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
		next, err := g.NetWait(ctx, st.Version)
		cancel()
		if err != nil {
			next = g.LANStatus()
		}
		st = next
	}
	return st
}

func playing(st NetStatus) bool { return st.Phase == PhasePlaying && st.Game != nil }

// connect hosts on h and joins from gu.
func connect(t *testing.T, h, gu *GameService) (NetStatus, NetStatus) {
	t.Helper()
	hs, err := h.HostLAN()
	if err != nil {
		t.Fatal(err)
	}
	if hs.Phase != PhaseHosting || len(hs.Code) != 9 {
		t.Fatalf("host status %+v", hs)
	}
	gs, err := gu.JoinLAN(hs.Code)
	if err != nil {
		t.Fatal(err)
	}
	if !playing(gs) || gs.Role != "guest" {
		t.Fatalf("guest status %+v", gs)
	}
	hs = waitNet(t, h, "host to see the guest", playing)
	if hs.Game.Human == gs.Game.Human || hs.Game.Human+gs.Game.Human != engine.Lava+engine.Ash {
		t.Fatalf("colours host %d guest %d", hs.Game.Human, gs.Game.Human)
	}
	return hs, gs
}

func freeHex(v *GameView) int {
	for c, x := range v.Cells {
		if x == 0 {
			return c
		}
	}
	return -1
}

// playOne makes the player to move place on the first free hex and waits for
// both devices to agree on the result.
func playOne(t *testing.T, h, gu *GameService) GameView {
	t.Helper()
	hv := h.LANStatus().Game
	mover, other := h, gu
	if hv.ToMove != hv.Human {
		mover, other = gu, h
	}
	mv := mover.LANStatus().Game
	if mv.Ply != hv.Ply {
		t.Fatalf("devices disagree before the move: ply %d vs %d", mv.Ply, hv.Ply)
	}
	// The player who is not to move is refused.
	ov := other.LANStatus().Game
	if _, err := other.Play(ov.ID, freeHex(ov)); code(err) != "not_your_turn" {
		t.Fatalf("out-of-turn move: %v", err)
	}
	cell := freeHex(mv)
	r, err := mover.Play(mv.ID, cell)
	if err != nil {
		t.Fatal(err)
	}
	ply := r.Game.Ply
	st := waitNet(t, other, "the rival's move", func(s NetStatus) bool { return s.Game != nil && s.Game.Ply == ply })
	if st.Remote == nil || st.Remote.Move != cell || st.Remote.Game.Ply != ply {
		t.Fatalf("rival's move not offered for animation: %+v", st.Remote)
	}
	waitNet(t, mover, "the move to settle", func(s NetStatus) bool { return s.Game != nil && s.Game.Ply == ply })
	a, b := h.LANStatus().Game, gu.LANStatus().Game
	for i := range a.Cells {
		if a.Cells[i] != b.Cells[i] {
			t.Fatalf("boards differ at %d after ply %d", i, ply)
		}
	}
	if a.Winner != b.Winner || a.ToMove != b.ToMove {
		t.Fatalf("winner %d/%d to move %d/%d", a.Winner, b.Winner, a.ToMove, b.ToMove)
	}
	return *a
}

func TestLANFullGameAndRematch(t *testing.T) {
	h, gu := lanPair(t)
	hs, gs := connect(t, h, gu)

	// No hints or undo across the network.
	if _, err := gu.Hint(context.Background(), gs.Game.ID); code(err) != "no_hints" {
		t.Fatalf("hint: %v", err)
	}
	if hs.Game.CanUndo || gs.Game.CanUndo {
		t.Fatal("undo offered in a network game")
	}

	v := playOne(t, h, gu)
	if _, err := h.Undo(v.ID); code(err) != "cannot_undo" {
		t.Fatalf("undo: %v", err)
	}
	for i := 0; i < 200 && v.Winner == 0; i++ {
		v = playOne(t, h, gu)
	}
	if v.Winner == 0 {
		t.Fatal("game did not end")
	}
	hv := waitNet(t, h, "host outcome", func(s NetStatus) bool { return s.Game.Outcome != nil }).Game
	gv := waitNet(t, gu, "guest outcome", func(s NetStatus) bool { return s.Game.Outcome != nil }).Game
	want := map[string]string{"win": "loss", "loss": "win", "draw": "draw"}
	if want[hv.Outcome.Result] != gv.Outcome.Result {
		t.Fatalf("outcomes %s / %s", hv.Outcome.Result, gv.Outcome.Result)
	}
	hp, _ := h.GetProfile()
	gp, _ := gu.GetProfile()
	if hp.Stats.LAN.Wins+hp.Stats.LAN.Losses+hp.Stats.LAN.Draws != 1 || gp.Stats.LAN.Wins+gp.Stats.LAN.Losses+gp.Stats.LAN.Draws != 1 {
		t.Fatalf("stats %+v / %+v", hp.Stats.LAN, gp.Stats.LAN)
	}
	if hp.Saved != nil || gp.Saved != nil {
		t.Fatal("a network game was saved for resuming")
	}
	if !contains(hp.Achievements, "lan") || !contains(gp.Achievements, "lan") {
		t.Fatal("the network achievement was not granted")
	}

	// Rematch: the second request starts it, and the colours swap.
	oldHost, oldGuest := hv.Human, gv.Human
	if _, err := gu.RematchLAN(true); err != nil {
		t.Fatal(err)
	}
	waitNet(t, h, "host to see the request", func(s NetStatus) bool { return s.PeerRematch })
	if _, err := h.RematchLAN(true); err != nil {
		t.Fatal(err)
	}
	nh := waitNet(t, h, "new game on host", func(s NetStatus) bool { return s.Game != nil && s.Game.ID != hv.ID })
	ng := waitNet(t, gu, "new game on guest", func(s NetStatus) bool { return s.Game != nil && s.Game.ID != gv.ID })
	if nh.Game.Human != oldGuest || ng.Game.Human != oldHost || nh.Game.Ply != 0 || ng.Game.Ply != 0 {
		t.Fatalf("rematch colours host %d guest %d (were %d/%d)", nh.Game.Human, ng.Game.Human, oldHost, oldGuest)
	}
	if nh.WantRematch || nh.PeerRematch || ng.WantRematch || ng.PeerRematch {
		t.Fatal("rematch requests carried into the new game")
	}
	playOne(t, h, gu)
}

func TestLANReconnect(t *testing.T) {
	h, gu := lanPair(t)
	connect(t, h, gu)
	playOne(t, h, gu)
	playOne(t, h, gu)

	// Drop the guest's connection as a Wi-Fi hiccup would.
	gu.mu.Lock()
	p := gu.m.peer
	gu.mu.Unlock()
	p.Close()
	// While the link is down, moves are refused on the guest.
	gs := waitNet(t, gu, "guest to notice", func(s NetStatus) bool { return s.Phase == PhaseLost })
	if _, err := gu.Play(gs.Game.ID, freeHex(gs.Game)); code(err) != "lan_offline" {
		t.Fatalf("move while offline: %v", err)
	}
	waitNet(t, gu, "guest to reconnect", playing)
	hs := waitNet(t, h, "host to see the guest again", playing)
	if hs.Game.Ply != 2 {
		t.Fatalf("ply after reconnect %d", hs.Game.Ply)
	}
	playOne(t, h, gu)
}

func TestLANLeaveCountsAsResignation(t *testing.T) {
	h, gu := lanPair(t)
	connect(t, h, gu)
	playOne(t, h, gu)
	st := gu.LeaveLAN()
	if st.Phase != PhaseOff {
		t.Fatalf("guest after leaving: %q", st.Phase)
	}
	hs := waitNet(t, h, "host to see the guest leave", func(s NetStatus) bool { return s.Phase == PhaseClosed })
	if hs.Reason != "left" || hs.Game == nil || hs.Game.Outcome == nil || hs.Game.Outcome.Result != "win" || hs.Game.Reason != "resign" {
		t.Fatalf("host after the guest left: %+v", hs)
	}
	gp, _ := gu.GetProfile()
	if gp.Stats.LAN.Losses != 1 {
		t.Fatalf("guest stats %+v", gp.Stats.LAN)
	}
	if _, err := h.RematchLAN(true); code(err) != "lan_offline" {
		t.Fatalf("rematch after the rival left: %v", err)
	}
	if h.LeaveLAN().Phase != PhaseOff {
		t.Fatal("host could not leave a closed match")
	}
}

func TestLANLeaveBeforeAnyMoveCountsForNobody(t *testing.T) {
	h, gu := lanPair(t)
	connect(t, h, gu)
	h.LeaveLAN()
	gs := waitNet(t, gu, "guest to see the host leave", func(s NetStatus) bool { return s.Phase == PhaseClosed })
	if gs.Reason != "left" {
		t.Fatalf("reason %q", gs.Reason)
	}
	hp, _ := h.GetProfile()
	gp, _ := gu.GetProfile()
	if hp.Stats.LAN != (LANStats{}) || gp.Stats.LAN != (LANStats{}) {
		t.Fatalf("stats %+v / %+v", hp.Stats.LAN, gp.Stats.LAN)
	}
}

func TestLANResignAcrossTheNetwork(t *testing.T) {
	h, gu := lanPair(t)
	_, gs := connect(t, h, gu)
	playOne(t, h, gu)
	v, err := gu.Resign(gs.Game.ID)
	if err != nil {
		t.Fatal(err)
	}
	if v.Outcome == nil || v.Outcome.Result != "loss" {
		t.Fatalf("guest view %+v", v.Outcome)
	}
	hs := waitNet(t, h, "host to see the resignation", func(s NetStatus) bool { return s.Game.Outcome != nil })
	if hs.Game.Outcome.Result != "win" || hs.Game.Reason != "resign" {
		t.Fatalf("host %+v", hs.Game)
	}
}

func TestLANThirdDeviceIsTurnedAway(t *testing.T) {
	h, gu := lanPair(t)
	hs, _ := connect(t, h, gu)
	third, _ := testService(t)
	defer third.ServiceShutdown()
	if _, err := third.JoinLAN(hs.Code); code(err) != "lan_busy" {
		t.Fatalf("third device: %v", err)
	}
	if third.LANStatus().Phase != PhaseOff {
		t.Fatal("a refused join left a match behind")
	}
}

func TestLANVersionMismatch(t *testing.T) {
	h, _ := lanPair(t)
	hs, err := h.HostLAN()
	if err != nil {
		t.Fatal(err)
	}
	ip, slot, _ := lan.Decode(hs.Code)
	c, err := net.Dial("tcp4", net.JoinHostPort(ip.String(), strconv.Itoa(lan.BasePort+slot)))
	if err != nil {
		t.Fatal(err)
	}
	p := lan.NewPeer(c)
	defer p.Close()
	p.Send(lan.Msg{T: "hello", V: lan.ProtocolVersion + 1})
	m, err := p.ReadWithin(2 * time.Second)
	if err != nil || m.T != "refuse" || m.Reason != "version" {
		t.Fatalf("got %+v %v", m, err)
	}
	if h.LANStatus().Phase != PhaseHosting {
		t.Fatal("a refused device ended the hosted game")
	}
}

func TestLANBadAndUnreachableCodes(t *testing.T) {
	g, _ := testService(t)
	if _, err := g.JoinLAN("ZZZZ"); code(err) != "lan_bad_code" {
		t.Fatalf("bad code: %v", err)
	}
	c, _ := lan.Encode(net.IPv4(127, 0, 0, 1), lan.PortSlots-1)
	if _, err := g.JoinLAN(c); code(err) != "lan_unreachable" {
		t.Fatalf("nobody hosting: %v", err)
	}
	if g.LANStatus().Phase != PhaseOff {
		t.Fatal("a failed join left a match behind")
	}
}

func TestLANVanishedRivalTimesOut(t *testing.T) {
	h, gu := lanPair(t)
	h.lanGrace = 300 * time.Millisecond
	connect(t, h, gu)
	playOne(t, h, gu)
	// The guest disappears without a word (app killed, out of range).
	gu.mu.Lock()
	m := gu.m
	gu.m = nil
	gu.mu.Unlock()
	m.stop()
	hs := waitNet(t, h, "host to give up", func(s NetStatus) bool { return s.Phase == PhaseClosed })
	if hs.Reason != "timeout" {
		t.Fatalf("reason %q", hs.Reason)
	}
	if hs.Game == nil || hs.Game.Outcome != nil {
		t.Fatal("an abandoned game was recorded")
	}
	if _, err := h.Play(hs.Game.ID, freeHex(hs.Game)); code(err) != "lan_offline" && code(err) != "not_your_turn" {
		t.Fatalf("move in an abandoned game: %v", err)
	}
}

func TestLANNewGameEndsTheMatch(t *testing.T) {
	h, gu := lanPair(t)
	connect(t, h, gu)
	playOne(t, h, gu)
	if _, err := h.NewGame(NewGameOptions{Mode: ModeHotseat}); err != nil {
		t.Fatal(err)
	}
	if h.LANStatus().Phase != PhaseOff {
		t.Fatal("the match survived a new local game")
	}
	gs := waitNet(t, gu, "guest to see the host go", func(s NetStatus) bool { return s.Phase == PhaseClosed })
	if gs.Game.Outcome == nil || gs.Game.Outcome.Result != "win" {
		t.Fatalf("guest %+v", gs.Game.Outcome)
	}
}

// The guest resigns while the host's move is on its way: the guest must take
// the host's move list, not call it a protocol error.
func TestLANResignCrossingTheHostsMove(t *testing.T) {
	h, gu := lanPair(t)
	_, gs := connect(t, h, gu)
	// Make it the host's turn.
	if hv := h.LANStatus().Game; hv.ToMove != hv.Human {
		playOne(t, h, gu)
	}
	if _, err := gu.Resign(gs.Game.ID); err != nil {
		t.Fatal(err)
	}
	gu.mu.Lock()
	m := gu.m
	s := gu.lanSessionLocked(m)
	moves := append(append([]int{}, s.moves...), freeHex(func() *GameView { v := s.view(); return &v }()))
	err := gu.applySyncLocked(m, lan.Msg{T: "sync", Game: m.gameNo, Color: m.guestColor, Moves: moves})
	// Check under the same lock: the host's real answer (which carries the
	// resignation and no extra move) may arrive right after and replace it.
	phase, v := m.phase, s.view()
	gu.mu.Unlock()
	if err != nil || phase == PhaseClosed {
		t.Fatalf("crossing sync: err %v phase %q", err, phase)
	}
	if v.Ply != len(moves) || v.Outcome == nil || v.Outcome.Result != "loss" {
		t.Fatalf("guest after the crossing: ply %d outcome %+v", v.Ply, v.Outcome)
	}
}

// A device that says hello and vanishes before anyone moves must not use up
// the hosted game: the host goes back to waiting and the real guest gets in.
func TestLANHostReopensWhenAGuestDropsEarly(t *testing.T) {
	h, _ := lanPair(t)
	hs, err := h.HostLAN()
	if err != nil {
		t.Fatal(err)
	}
	ip, slot, _ := lan.Decode(hs.Code)
	c, err := net.Dial("tcp4", net.JoinHostPort(ip.String(), strconv.Itoa(lan.BasePort+slot)))
	if err != nil {
		t.Fatal(err)
	}
	p := lan.NewPeer(c)
	p.Send(lan.Msg{T: "hello", V: lan.ProtocolVersion})
	if m, err := p.ReadWithin(2 * time.Second); err != nil || m.T != "welcome" {
		t.Fatalf("got %+v %v", m, err)
	}
	waitNet(t, h, "the drive-by to join", playing)
	p.Close()
	st := waitNet(t, h, "host to reopen", func(s NetStatus) bool { return s.Phase == PhaseHosting })
	if st.Game != nil {
		t.Fatal("the abandoned empty game is still shown")
	}
	real, _ := testService(t)
	defer real.ServiceShutdown()
	if _, err := real.JoinLAN(hs.Code); err != nil {
		t.Fatalf("the real guest was turned away: %v", err)
	}
	waitNet(t, h, "the real guest", playing)
	hp, _ := h.GetProfile()
	if hp.Stats.LAN != (LANStats{}) {
		t.Fatalf("an empty game was recorded: %+v", hp.Stats.LAN)
	}
}

// A guest that leaves before any move frees the seat too.
func TestLANGuestLeavingAnEmptyGameReopensTheHost(t *testing.T) {
	h, gu := lanPair(t)
	hs, _ := connect(t, h, gu)
	gu.LeaveLAN()
	waitNet(t, h, "host to reopen", func(s NetStatus) bool { return s.Phase == PhaseHosting && s.Game == nil })
	if _, err := gu.JoinLAN(hs.Code); err != nil {
		t.Fatalf("rejoin: %v", err)
	}
	waitNet(t, h, "host to start again", playing)
}

// Leaving while the link is down records nothing, as the rival (who will time
// out) records nothing either.
func TestLANLeavingWhileOfflineRecordsNothing(t *testing.T) {
	h, gu := lanPair(t)
	connect(t, h, gu)
	playOne(t, h, gu)
	gu.mu.Lock()
	p := gu.m.peer
	gu.mu.Unlock()
	p.Close()
	waitNet(t, gu, "guest to notice", func(s NetStatus) bool { return s.Phase == PhaseLost })
	gu.LeaveLAN()
	gp, _ := gu.GetProfile()
	if gp.Stats.LAN != (LANStats{}) {
		t.Fatalf("leaving offline was recorded: %+v", gp.Stats.LAN)
	}
}
