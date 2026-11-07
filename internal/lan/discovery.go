package lan

import (
	"encoding/json"
	"net"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// DeviceName is a friendly name to show next to a hosted game, or "" when
// the system only says "localhost" (Android does).
func DeviceName() string {
	h, err := os.Hostname()
	if err != nil {
		return ""
	}
	h = strings.TrimSpace(h)
	if h == "" || strings.EqualFold(h, "localhost") || len(h) > 40 {
		return ""
	}
	return h
}

// beacon is a host's answer to a probe.
type beacon struct {
	Basalt int    `json:"basalt"` // protocol version
	Probe  bool   `json:"probe,omitempty"`
	Code   string `json:"code,omitempty"`
	Name   string `json:"name,omitempty"`
	Port   int    `json:"port,omitempty"`
}

// Advertiser answers "who is hosting?" probes so that devices on the same
// network can list this game without typing its code.
type Advertiser struct {
	pc   net.PacketConn
	once sync.Once
}

// Advertise listens for probes on DiscoveryPort and answers each with code,
// name and the TCP port to connect to. The answer goes back to the sender
// directly, so it leaves through whichever adapter (Wi-Fi or Ethernet) the
// probe came in on.
func Advertise(code, name string, port int) (*Advertiser, error) {
	pc, err := net.ListenPacket("udp4", ":"+strconv.Itoa(DiscoveryPort))
	if err != nil {
		return nil, err
	}
	a := &Advertiser{pc: pc}
	reply, _ := json.Marshal(beacon{Basalt: ProtocolVersion, Code: Normalize(code), Name: name, Port: port})
	go func() {
		buf := make([]byte, 512)
		for {
			n, from, err := pc.ReadFrom(buf)
			if err != nil {
				if ne, ok := err.(net.Error); ok && ne.Timeout() {
					continue
				}
				return // closed
			}
			var b beacon
			if json.Unmarshal(buf[:n], &b) != nil || !b.Probe || b.Basalt != ProtocolVersion {
				continue
			}
			_, _ = pc.WriteTo(reply, from)
		}
	}()
	return a, nil
}

// Close stops answering probes.
func (a *Advertiser) Close() {
	if a == nil {
		return
	}
	a.once.Do(func() { _ = a.pc.Close() })
}

// Found is a game seen on the local network.
type Found struct {
	Code string // normalised, no dash
	Name string
	Addr string // ip:port to connect to
	seen time.Time
}

// Browser keeps probing the local network for hosted games.
type Browser struct {
	pc net.PacketConn

	mu       sync.Mutex
	found    map[string]Found
	lastPoll time.Time
	once     sync.Once
	done     chan struct{}
}

const (
	probeEvery = time.Second
	foundTTL   = 4 * time.Second
	browseIdle = 30 * time.Second // stop probing if nobody asks for results
	maxFound   = 32
)

// Browse starts probing every adapter's broadcast address (so a PC on
// Ethernet finds a phone on the router's Wi-Fi and vice versa), the
// all-ones broadcast, and this machine itself.
func Browse() (*Browser, error) {
	pc, err := net.ListenPacket("udp4", ":0")
	if err != nil {
		return nil, err
	}
	b := &Browser{pc: pc, found: map[string]Found{}, lastPoll: time.Now(), done: make(chan struct{})}
	go b.probeLoop()
	go b.readLoop()
	return b, nil
}

func (b *Browser) probeLoop() {
	probe, _ := json.Marshal(beacon{Basalt: ProtocolVersion, Probe: true})
	t := time.NewTicker(probeEvery)
	defer t.Stop()
	for {
		targets := map[string]bool{"255.255.255.255": true, "127.0.0.1": true}
		for _, a := range LocalAddrs() {
			targets[a.Broadcast.String()] = true
		}
		for ip := range targets {
			_, _ = b.pc.WriteTo(probe, &net.UDPAddr{IP: net.ParseIP(ip), Port: DiscoveryPort})
		}
		select {
		case <-b.done:
			return
		case <-t.C:
		}
		b.mu.Lock()
		idle := time.Since(b.lastPoll) > browseIdle
		b.mu.Unlock()
		if idle {
			b.Close()
			return
		}
	}
}

func (b *Browser) readLoop() {
	buf := make([]byte, 512)
	for {
		n, from, err := b.pc.ReadFrom(buf)
		if err != nil {
			if ne, ok := err.(net.Error); ok && ne.Timeout() {
				continue
			}
			return
		}
		ua, ok := from.(*net.UDPAddr)
		if !ok {
			continue
		}
		var bc beacon
		if json.Unmarshal(buf[:n], &bc) != nil || bc.Probe || bc.Basalt != ProtocolVersion || bc.Port <= 0 || bc.Port > 65535 {
			continue
		}
		if _, _, err := Decode(bc.Code); err != nil {
			continue
		}
		code := Normalize(bc.Code)
		name := bc.Name
		if len(name) > 40 {
			name = name[:40]
		}
		f := Found{Code: code, Name: name, Addr: net.JoinHostPort(ua.IP.String(), strconv.Itoa(bc.Port)), seen: time.Now()}
		b.mu.Lock()
		// Forget games that stopped answering, and never hold more than a
		// screenful, whatever another device on the network sends.
		for c, old := range b.found {
			if time.Since(old.seen) > 3*foundTTL {
				delete(b.found, c)
			}
		}
		if _, known := b.found[code]; !known && len(b.found) >= maxFound {
			b.mu.Unlock()
			continue
		}
		// A game on this machine answers both on loopback and on the LAN;
		// keep the LAN address so the code works the same way for everyone.
		if old, ok := b.found[code]; !ok || !ua.IP.IsLoopback() || time.Since(old.seen) > foundTTL {
			b.found[code] = f
		} else {
			old.seen = f.seen
			b.found[code] = old
		}
		b.mu.Unlock()
	}
}

// Games lists the games seen in the last few seconds, sorted by code.
func (b *Browser) Games() []Found {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.lastPoll = time.Now()
	out := []Found{}
	for _, f := range b.found {
		if time.Since(f.seen) <= foundTTL {
			out = append(out, f)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Code < out[j].Code })
	return out
}

// Lookup returns the address a game with this code answered from.
func (b *Browser) Lookup(code string) (Found, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	f, ok := b.found[Normalize(code)]
	return f, ok && time.Since(f.seen) <= 3*foundTTL
}

// Closed reports whether the browser has stopped (closed or idle).
func (b *Browser) Closed() bool {
	select {
	case <-b.done:
		return true
	default:
		return false
	}
}

// Close stops probing.
func (b *Browser) Close() {
	if b == nil {
		return
	}
	b.once.Do(func() {
		close(b.done)
		_ = b.pc.Close()
	})
}
