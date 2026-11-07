package lan

import (
	"bufio"
	"encoding/json"
	"errors"
	"net"
	"sync"
	"time"
)

// ProtocolVersion must match on both devices; a host refuses other versions.
const ProtocolVersion = 1

const (
	pingEvery   = 2 * time.Second
	ReadTimeout = 7 * time.Second // no traffic (not even a ping) for this long = gone
	writeLimit  = 5 * time.Second
	maxLine     = 16 << 10
	queueSize   = 64
)

// Msg is one line on the wire. Fields are shared between message types and
// omitted when empty.
//
//	guest → host: hello{v, token?}, move{game, ply, cell}, resign{game},
//	              rematch{game, want}, leave
//	host → guest: welcome{token, + sync fields}, sync{game, color, moves,
//	              resigned, wantHost, wantGuest}, refuse{reason}, leave
//	both:         ping (every 2 s; any line resets the read deadline)
type Msg struct {
	T         string `json:"t"`
	V         int    `json:"v,omitempty"`
	Token     string `json:"token,omitempty"`
	Reason    string `json:"reason,omitempty"`
	Game      int    `json:"game,omitempty"`
	Color     int8   `json:"color,omitempty"` // the guest's colour in this game
	Moves     []int  `json:"moves,omitempty"`
	Resigned  int8   `json:"resigned,omitempty"`
	Ply       int    `json:"ply,omitempty"`
	Cell      int    `json:"cell,omitempty"`
	Want      bool   `json:"want,omitempty"`
	WantHost  bool   `json:"wantHost,omitempty"`
	WantGuest bool   `json:"wantGuest,omitempty"`
}

// ErrClosed is returned by Read once the peer has been closed locally.
var ErrClosed = errors.New("lan: connection closed")

// Peer is one live connection. Sends never block: they are queued for a
// writer goroutine, and a peer that cannot keep up is dropped. Read is meant
// for a single reader goroutine.
type Peer struct {
	conn net.Conn
	sc   *bufio.Scanner
	out  chan []byte // nil entry = flush and close

	mu      sync.Mutex
	closing bool
	done    chan struct{}
	once    sync.Once
}

// NewPeer wraps c and starts its writer and heartbeat.
func NewPeer(c net.Conn) *Peer {
	if tc, ok := c.(*net.TCPConn); ok {
		_ = tc.SetNoDelay(true)
	}
	sc := bufio.NewScanner(c)
	sc.Buffer(make([]byte, 4096), maxLine)
	p := &Peer{conn: c, sc: sc, out: make(chan []byte, queueSize), done: make(chan struct{})}
	go p.writer()
	go p.heartbeat()
	return p
}

// Send queues m. It reports false if the peer is closed or hopelessly behind
// (in which case it is closed).
func (p *Peer) Send(m Msg) bool {
	b, err := json.Marshal(m)
	if err != nil {
		return false
	}
	b = append(b, '\n')
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closing {
		return false
	}
	select {
	case p.out <- b:
		return true
	default:
		p.closing = true
		go p.Close()
		return false
	}
}

// Finish queues m (if any) as the last message, then closes once it is out.
func (p *Peer) Finish(m *Msg) {
	if m != nil {
		p.Send(*m)
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closing {
		return
	}
	p.closing = true
	select {
	case p.out <- nil:
	default:
		go p.Close()
	}
	// Don't wait forever for a stuck writer.
	time.AfterFunc(writeLimit, p.Close)
}

// Read returns the next message that is not a heartbeat. It fails when
// nothing at all arrives for ReadTimeout.
func (p *Peer) Read() (Msg, error) {
	return p.ReadWithin(ReadTimeout)
}

// ReadWithin is Read with a custom silence limit (handshakes use a short one).
func (p *Peer) ReadWithin(limit time.Duration) (Msg, error) {
	for {
		if err := p.conn.SetReadDeadline(time.Now().Add(limit)); err != nil {
			return Msg{}, err
		}
		if !p.sc.Scan() {
			select {
			case <-p.done:
				return Msg{}, ErrClosed
			default:
			}
			if err := p.sc.Err(); err != nil {
				return Msg{}, err
			}
			return Msg{}, net.ErrClosed
		}
		var m Msg
		if err := json.Unmarshal(p.sc.Bytes(), &m); err != nil {
			return Msg{}, err
		}
		if m.T == "ping" {
			continue
		}
		return m, nil
	}
}

// Close drops the connection immediately. Safe to call more than once.
func (p *Peer) Close() {
	p.once.Do(func() {
		p.mu.Lock()
		p.closing = true
		p.mu.Unlock()
		close(p.done)
		_ = p.conn.Close()
	})
}

// Done is closed once the peer is closed.
func (p *Peer) Done() <-chan struct{} { return p.done }

// RemoteIP is the address the other device connected from.
func (p *Peer) RemoteIP() string {
	if a, ok := p.conn.RemoteAddr().(*net.TCPAddr); ok {
		return a.IP.String()
	}
	return ""
}

func (p *Peer) writer() {
	for {
		select {
		case <-p.done:
			return
		case b := <-p.out:
			if b == nil {
				p.linger()
				return
			}
			_ = p.conn.SetWriteDeadline(time.Now().Add(writeLimit))
			if _, err := p.conn.Write(b); err != nil {
				p.Close()
				return
			}
		}
	}
}

// linger ends a Finish gracefully. Closing a socket that still has unread
// incoming bytes (the rival's heartbeats) sends a reset, and a reset can make
// the other side drop our last message before reading it (Windows does).
// So: send FIN, read and discard until the other side closes too or a short
// wait passes, then close.
func (p *Peer) linger() {
	tc, ok := p.conn.(*net.TCPConn)
	if !ok {
		p.Close()
		return
	}
	_ = tc.CloseWrite()
	go func() {
		defer p.Close()
		_ = tc.SetReadDeadline(time.Now().Add(2 * time.Second))
		buf := make([]byte, 1024)
		for {
			if _, err := tc.Read(buf); err != nil {
				return
			}
		}
	}()
}

func (p *Peer) heartbeat() {
	t := time.NewTicker(pingEvery)
	defer t.Stop()
	for {
		select {
		case <-p.done:
			return
		case <-t.C:
			p.Send(Msg{T: "ping"})
		}
	}
}
