//go:build android

package lan

import (
	"encoding/binary"
	"net"
	"strings"
	"syscall"
	"unsafe"
)

// platformAddrs lists IPv4 addresses with a netlink RTM_GETADDR dump on a
// socket that is never bound. Since Android 11, apps may not bind netlink
// sockets or ask for RTM_GETLINK, which is how net.Interfaces works, but an
// unbound address dump is still allowed. This is what finds a hotspot's
// address on a phone whose default route is mobile data.
func platformAddrs() []LocalAddr {
	fd, err := syscall.Socket(syscall.AF_NETLINK, syscall.SOCK_RAW|syscall.SOCK_CLOEXEC, syscall.NETLINK_ROUTE)
	if err != nil {
		return nil
	}
	defer syscall.Close(fd)
	_ = syscall.SetsockoptTimeval(fd, syscall.SOL_SOCKET, syscall.SO_RCVTIMEO, &syscall.Timeval{Sec: 1})

	req := make([]byte, syscall.NLMSG_HDRLEN+syscall.SizeofRtGenmsg)
	binary.NativeEndian.PutUint32(req[0:], uint32(len(req)))
	binary.NativeEndian.PutUint16(req[4:], syscall.RTM_GETADDR)
	binary.NativeEndian.PutUint16(req[6:], syscall.NLM_F_REQUEST|syscall.NLM_F_DUMP)
	binary.NativeEndian.PutUint32(req[8:], 1) // sequence
	req[syscall.NLMSG_HDRLEN] = syscall.AF_INET
	if err := syscall.Sendto(fd, req, 0, &syscall.SockaddrNetlink{Family: syscall.AF_NETLINK}); err != nil {
		return nil
	}

	var out []LocalAddr
	buf := make([]byte, 1<<16)
	for range 64 { // a dump is a handful of datagrams; never loop forever
		n, _, err := syscall.Recvfrom(fd, buf, 0)
		if err != nil || n < syscall.NLMSG_HDRLEN {
			return out
		}
		msgs, err := syscall.ParseNetlinkMessage(buf[:n])
		if err != nil {
			return out
		}
		for i := range msgs {
			m := &msgs[i]
			switch m.Header.Type {
			case syscall.NLMSG_DONE, syscall.NLMSG_ERROR:
				return out
			case syscall.RTM_NEWADDR:
			default:
				continue
			}
			if len(m.Data) < syscall.SizeofIfAddrmsg {
				continue
			}
			ifa := (*syscall.IfAddrmsg)(unsafe.Pointer(&m.Data[0]))
			if ifa.Family != syscall.AF_INET {
				continue
			}
			attrs, err := syscall.ParseNetlinkRouteAttr(m)
			if err != nil {
				continue
			}
			var ip, addr net.IP
			var name string
			for _, a := range attrs {
				switch a.Attr.Type {
				case syscall.IFA_LOCAL:
					if len(a.Value) >= 4 {
						ip = net.IPv4(a.Value[0], a.Value[1], a.Value[2], a.Value[3]).To4()
					}
				case syscall.IFA_ADDRESS:
					if len(a.Value) >= 4 {
						addr = net.IPv4(a.Value[0], a.Value[1], a.Value[2], a.Value[3]).To4()
					}
				case syscall.IFA_LABEL:
					name = strings.TrimRight(string(a.Value), "\x00")
				}
			}
			if ip == nil {
				ip = addr
			}
			if !usable(ip) || name == "lo" {
				continue
			}
			mask := net.CIDRMask(int(ifa.Prefixlen), 32)
			out = append(out, LocalAddr{IP: ip, Broadcast: broadcastOf(ip, mask), Name: name})
		}
	}
	return out
}
