package lan

import (
	"net"
	"strings"
	"testing"
	"time"
)

func TestCodeRoundTrip(t *testing.T) {
	cases := []struct {
		ip   string
		slot int
	}{
		{"192.168.1.23", 0}, {"10.0.0.2", 15}, {"172.20.10.1", 3}, {"192.168.43.1", 7}, {"192.168.137.1", 1}, {"127.0.0.1", 0}, {"255.255.255.254", 9},
	}
	for _, c := range cases {
		code, err := Encode(net.ParseIP(c.ip), c.slot)
		if err != nil {
			t.Fatalf("%s: %v", c.ip, err)
		}
		if len(code) != 9 || code[4] != '-' {
			t.Fatalf("%s: code %q is not ABCD-EFGH", c.ip, code)
		}
		for _, typed := range []string{code, strings.ToLower(code), strings.ReplaceAll(code, "-", " "), " " + code + " "} {
			ip, slot, err := Decode(typed)
			if err != nil || !ip.Equal(net.ParseIP(c.ip)) || slot != c.slot {
				t.Fatalf("Decode(%q) = %v %d %v, want %s %d", typed, ip, slot, err, c.ip, c.slot)
			}
		}
	}
}

func TestCodeLookalikes(t *testing.T) {
	code, _ := Encode(net.ParseIP("10.1.0.1"), 0)
	// Typing O for 0 and l for 1 still works.
	typed := strings.NewReplacer("0", "O", "1", "l").Replace(code)
	if ip, _, err := Decode(typed); err != nil || !ip.Equal(net.ParseIP("10.1.0.1")) {
		t.Fatalf("Decode(%q) = %v, %v", typed, ip, err)
	}
}

func TestCodeRejectsTypos(t *testing.T) {
	code, _ := Encode(net.ParseIP("192.168.1.23"), 2)
	s := Normalize(code)
	caught, total := 0, 0
	for i := 0; i < len(s); i++ {
		for _, r := range alphabet {
			if byte(r) == s[i] {
				continue
			}
			typo := s[:i] + string(r) + s[i+1:]
			total++
			if _, _, err := Decode(typo); err != nil {
				caught++
			}
		}
	}
	// A 4-bit check catches about 15 of 16 random mistakes.
	if float64(caught)/float64(total) < 0.85 {
		t.Fatalf("only %d of %d single-character typos rejected", caught, total)
	}
	for _, bad := range []string{"", "ABC", "ABCD-EFGHJ", "ABCD-EFGU", "!!!!-!!!!"} {
		if _, _, err := Decode(bad); err == nil {
			t.Errorf("Decode(%q) accepted", bad)
		}
	}
	if _, err := Encode(net.ParseIP("::1"), 0); err == nil {
		t.Error("Encode accepted IPv6")
	}
	if _, err := Encode(net.ParseIP("10.0.0.1"), PortSlots); err == nil {
		t.Error("Encode accepted an out-of-range slot")
	}
}

func TestBroadcastOf(t *testing.T) {
	cases := []struct {
		ip, want string
		bits     int
	}{
		{"192.168.1.23", "192.168.1.255", 24},
		{"10.4.5.6", "10.4.255.255", 16},
		{"172.20.10.2", "172.20.10.15", 28}, // iPhone hotspot
	}
	for _, c := range cases {
		got := broadcastOf(net.ParseIP(c.ip), net.CIDRMask(c.bits, 32))
		if got.String() != c.want {
			t.Errorf("broadcastOf(%s/%d) = %s, want %s", c.ip, c.bits, got, c.want)
		}
	}
	if got := broadcastOf(net.ParseIP("192.168.43.1"), nil); got.String() != "192.168.43.255" {
		t.Errorf("no mask: %s", got)
	}
}

func TestRankPrefersRealNetworks(t *testing.T) {
	cases := []struct {
		ip, name string
		want     int
	}{
		{"100.72.1.9", "rmnet_data0", 3}, // mobile data (carrier NAT)
		{"10.33.4.2", "rmnet_data1", 3},  // mobile data in a private range
		{"172.28.0.1", "vEthernet (WSL)", 2},
		{"192.168.43.1", "wlan1", 0},   // the phone's hotspot
		{"192.168.137.1", "Local Area Connection* 10", 0}, // Windows mobile hotspot
		{"192.168.1.40", "Ethernet", 0},
		{"192.168.1.41", "", 1},
	}
	for _, c := range cases {
		if got := rank(LocalAddr{IP: net.ParseIP(c.ip).To4(), Name: c.name}); got != c.want {
			t.Errorf("rank(%s %q) = %d, want %d", c.ip, c.name, got, c.want)
		}
	}
}

func TestLocalAddrsOnThisMachine(t *testing.T) {
	for _, a := range LocalAddrs() {
		code, _ := Encode(a.IP, 0)
		t.Logf("%-16s bcast %-16s rank %d %-30q %s", a.IP, a.Broadcast, rank(a), a.Name, code)
		if !usable(a.IP) || a.Broadcast.To4() == nil {
			t.Errorf("unusable address %+v", a)
		}
	}
}

func pipePeers(t *testing.T) (*Peer, *Peer) {
	t.Helper()
	ln, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	ch := make(chan net.Conn, 1)
	go func() {
		c, _ := ln.Accept()
		ch <- c
	}()
	c, err := net.Dial("tcp4", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	a, b := NewPeer(c), NewPeer(<-ch)
	t.Cleanup(func() { a.Close(); b.Close() })
	return a, b
}

func TestPeerSendReadFinish(t *testing.T) {
	a, b := pipePeers(t)
	a.Send(Msg{T: "move", Game: 1, Ply: 0, Cell: 0})
	a.Send(Msg{T: "move", Game: 1, Ply: 1, Cell: 60, Moves: []int{3, 0, 60}})
	m, err := b.ReadWithin(time.Second)
	if err != nil || m.T != "move" || m.Cell != 0 || m.Ply != 0 {
		t.Fatalf("first read %+v %v", m, err)
	}
	m, err = b.ReadWithin(time.Second)
	if err != nil || m.Cell != 60 || len(m.Moves) != 3 || m.Moves[1] != 0 {
		t.Fatalf("second read %+v %v", m, err)
	}
	a.Finish(&Msg{T: "leave"})
	if m, err = b.ReadWithin(time.Second); err != nil || m.T != "leave" {
		t.Fatalf("finish read %+v %v", m, err)
	}
	if _, err = b.ReadWithin(time.Second); err == nil {
		t.Fatal("read after the other side closed succeeded")
	}
	if a.Send(Msg{T: "move"}) {
		t.Fatal("send after Finish was accepted")
	}
}

func TestPeerSkipsHeartbeats(t *testing.T) {
	a, b := pipePeers(t)
	// Nothing but pings for longer than the ping interval: Read keeps waiting.
	go func() {
		time.Sleep(pingEvery + 500*time.Millisecond)
		a.Send(Msg{T: "sync", Game: 2})
	}()
	m, err := b.ReadWithin(pingEvery * 2)
	if err != nil || m.T != "sync" || m.Game != 2 {
		t.Fatalf("got %+v %v", m, err)
	}
}

func TestPeerRejectsHugeLines(t *testing.T) {
	a, b := pipePeers(t)
	go func() { _, _ = a.conn.Write([]byte(strings.Repeat("x", maxLine+10) + "\n")) }()
	if _, err := b.ReadWithin(time.Second); err == nil {
		t.Fatal("an oversized line was accepted")
	}
}

func TestDiscoveryOnLoopback(t *testing.T) {
	code, _ := Encode(net.ParseIP("127.0.0.1"), 4)
	adv, err := Advertise(code, "test-pc", BasePort+4)
	if err != nil {
		t.Skipf("discovery port busy: %v", err)
	}
	defer adv.Close()
	b, err := Browse()
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		for _, f := range b.Games() {
			if f.Code == Normalize(code) {
				if f.Name != "test-pc" || !strings.HasSuffix(f.Addr, ":47735") {
					t.Fatalf("found %+v", f)
				}
				if _, ok := b.Lookup(Format(code)); !ok {
					t.Fatal("Lookup missed a listed game")
				}
				return
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatal("the advertised game was not found")
}
