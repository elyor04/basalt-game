package lan

import (
	"net"
	"sort"
	"strings"
)

// LocalAddr is one IPv4 address of this device on a local network (Wi-Fi,
// Ethernet, a phone or PC hotspot...) and that network's broadcast address.
type LocalAddr struct {
	IP        net.IP
	Broadcast net.IP
	Name      string // adapter name, "" when unknown
}

// LocalAddrs lists this device's local-network IPv4 addresses, best first.
// Loopback and link-local addresses are left out.
//
// "Best" is the address another device on the same Wi-Fi, cable or hotspot
// is most likely to reach: real adapters before VPNs and virtual machines,
// and mobile data last (a phone sharing a hotspot while on mobile data has
// its default route on mobile data, which no nearby device can use).
func LocalAddrs() []LocalAddr {
	list := adapterAddrs()
	if ip := routeIP(); ip != nil {
		found := false
		for i := range list {
			if list[i].IP.Equal(ip) {
				found = true
				// The default route's adapter goes first among equals.
				list[0], list[i] = list[i], list[0]
				break
			}
		}
		if !found {
			list = append([]LocalAddr{{IP: ip, Broadcast: broadcastOf(ip, nil)}}, list...)
		}
	}
	sort.SliceStable(list, func(i, j int) bool { return rank(list[i]) < rank(list[j]) })
	return list
}

// rank orders addresses: 0 private on a physical adapter, 1 private on an
// unknown adapter, 2 virtual/VPN, 3 mobile data or public.
func rank(a LocalAddr) int {
	n := strings.ToLower(a.Name)
	for _, p := range []string{"rmnet", "ccmni", "pdp", "clat", "v4-", "seth", "wwan", "usb"} {
		if strings.HasPrefix(n, p) {
			return 3
		}
	}
	if !a.IP.IsPrivate() {
		return 3
	}
	for _, p := range []string{"vethernet", "virtualbox", "vmware", "vmnet", "hyper-v", "docker", "br-", "veth", "virbr", "tailscale", "zerotier", "wireguard", "tun", "tap", "utun", "wsl", "ham", "npcap"} {
		if strings.Contains(n, p) {
			return 2
		}
	}
	if n == "" {
		return 1
	}
	return 0
}

// routeIP is the address of the adapter holding the default route. It
// "connects" a UDP socket, which sends nothing and needs no permission.
func routeIP() net.IP {
	c, err := net.Dial("udp4", "192.0.2.1:9") // TEST-NET-1: never reached
	if err != nil {
		return nil
	}
	defer c.Close()
	if a, ok := c.LocalAddr().(*net.UDPAddr); ok && usable(a.IP) {
		return a.IP.To4()
	}
	return nil
}

// adapterAddrs lists every up, non-loopback adapter's IPv4 addresses. Where
// the standard library cannot list adapters (Android 11+ forbids the netlink
// call it uses), platformAddrs reads them another way.
func adapterAddrs() []LocalAddr {
	var out []LocalAddr
	ifs, err := net.Interfaces()
	if err != nil {
		return dedupe(platformAddrs())
	}
	for _, ifc := range ifs {
		if ifc.Flags&net.FlagUp == 0 || ifc.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, err := ifc.Addrs()
		if err != nil {
			continue
		}
		for _, a := range addrs {
			if n, ok := a.(*net.IPNet); ok && usable(n.IP) {
				ip := n.IP.To4()
				out = append(out, LocalAddr{IP: ip, Broadcast: broadcastOf(ip, n.Mask), Name: ifc.Name})
			}
		}
	}
	if len(out) == 0 {
		out = platformAddrs()
	}
	return dedupe(out)
}

func usable(ip net.IP) bool {
	ip = ip.To4()
	return ip != nil && !ip.IsLoopback() && !ip.IsLinkLocalUnicast() && !ip.IsUnspecified() && !ip.IsMulticast()
}

func dedupe(in []LocalAddr) []LocalAddr {
	var out []LocalAddr
	for _, a := range in {
		dup := false
		for _, b := range out {
			if a.IP.Equal(b.IP) {
				dup = true
				break
			}
		}
		if !dup {
			out = append(out, a)
		}
	}
	return out
}

// broadcastOf is the directed broadcast address of ip's network. Without a
// mask it assumes /24, which most home networks and hotspots use.
func broadcastOf(ip net.IP, mask net.IPMask) net.IP {
	ip = ip.To4()
	if len(mask) == 16 {
		mask = mask[12:]
	}
	if len(mask) != 4 {
		mask = net.CIDRMask(24, 32)
	}
	b := make(net.IP, 4)
	for i := range b {
		b[i] = ip[i] | ^mask[i]
	}
	return b
}
