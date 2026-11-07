// Package lan is the same-Wi-Fi transport for two-device games: join codes,
// finding games on the local network, and a framed, heart-beating JSON
// connection. It knows nothing about the rules; internal/game drives it.
package lan

import (
	"errors"
	"net"
	"strings"
)

const (
	// BasePort is the first TCP port a host tries. A host takes the first
	// free one of PortSlots consecutive ports and the slot goes in the code.
	BasePort  = 47731
	PortSlots = 16
	// DiscoveryPort is the UDP port hosts listen on for "who is hosting?"
	// probes from devices looking for a game.
	DiscoveryPort = 47750
)

// ErrBadCode is returned for a join code that cannot be decoded.
var ErrBadCode = errors.New("lan: malformed join code")

// Crockford base32: no I, L, O or U, so codes read aloud are unambiguous.
const alphabet = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"

// Encode packs an IPv4 address and a port slot into an 8-character code
// shown as "ABCD-EFGH": 32 address bits, 4 slot bits and a 4-bit checksum.
func Encode(ip net.IP, slot int) (string, error) {
	v4 := ip.To4()
	if v4 == nil || slot < 0 || slot >= PortSlots {
		return "", ErrBadCode
	}
	v := uint64(v4[0])<<28 | uint64(v4[1])<<20 | uint64(v4[2])<<12 | uint64(v4[3])<<4 | uint64(slot)
	full := v<<4 | checksum(v)
	var b [8]byte
	for i := 7; i >= 0; i-- {
		b[i] = alphabet[full&31]
		full >>= 5
	}
	return string(b[:4]) + "-" + string(b[4:]), nil
}

// Decode reverses Encode. It accepts lower case, spaces and dashes, and the
// usual look-alikes (O for 0, I and L for 1).
func Decode(code string) (net.IP, int, error) {
	s := Normalize(code)
	if len(s) != 8 {
		return nil, 0, ErrBadCode
	}
	var full uint64
	for i := 0; i < len(s); i++ {
		d := strings.IndexByte(alphabet, s[i])
		if d < 0 {
			return nil, 0, ErrBadCode
		}
		full = full<<5 | uint64(d)
	}
	v := full >> 4
	if full&15 != checksum(v) {
		return nil, 0, ErrBadCode
	}
	ip := net.IPv4(byte(v>>28), byte(v>>20), byte(v>>12), byte(v>>4)).To4()
	if ip.IsUnspecified() || ip.IsMulticast() || ip.Equal(net.IPv4bcast) {
		return nil, 0, ErrBadCode
	}
	return ip, int(v & 15), nil
}

// Normalize upper-cases a typed code, drops separators and maps look-alike
// letters onto digits. The result has no dash.
func Normalize(code string) string {
	var b strings.Builder
	for _, r := range strings.ToUpper(code) {
		switch r {
		case ' ', '-', '\t', '_', '.':
			continue
		case 'O':
			r = '0'
		case 'I', 'L':
			r = '1'
		}
		b.WriteRune(r)
	}
	return b.String()
}

// Format turns a code back into "ABCD-EFGH" form.
func Format(code string) string {
	s := Normalize(code)
	if len(s) != 8 {
		return s
	}
	return s[:4] + "-" + s[4:]
}

// checksum is a rotate-and-xor over the nine nibbles of v, so any single
// mistyped character changes it, and most swaps of neighbours do too.
func checksum(v uint64) uint64 {
	var c uint64
	for i := 8; i >= 0; i-- {
		c = ((c<<1 | c>>3) & 15) ^ (v >> (4 * i) & 15)
		c = (c + uint64(i) + 1) & 15
	}
	return c
}
