//go:build !android

package lan

// platformAddrs is only needed where net.Interfaces is not allowed (Android).
func platformAddrs() []LocalAddr { return nil }
