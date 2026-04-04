//go:build windows

package proxy

import (
	"fmt"
	"math/bits"
	"net"
	"syscall"

	"golang.org/x/sys/windows"
)

const (
	ipUnicastIf   = 31
	ipv6UnicastIf = 31
)

func configureDialerInterfacePinning(dialer *net.Dialer, ifaceIndex int, bindIP net.IP) error {
	if dialer == nil {
		return nil
	}
	if ifaceIndex <= 0 || bindIP == nil {
		return nil
	}

	boundIPv4 := bindIP.To4() != nil
	dialer.Control = func(_, _ string, rawConn syscall.RawConn) error {
		var controlErr error
		err := rawConn.Control(func(fd uintptr) {
			handle := windows.Handle(fd)
			if boundIPv4 {
				// Windows expects IPv4 interface index in network byte order for IP_UNICAST_IF.
				iface := int(bits.ReverseBytes32(uint32(ifaceIndex)))
				controlErr = windows.SetsockoptInt(handle, windows.IPPROTO_IP, ipUnicastIf, iface)
				if controlErr != nil {
					controlErr = fmt.Errorf("setsockopt IP_UNICAST_IF=%d: %w", ifaceIndex, controlErr)
				}
				return
			}

			// IPV6_UNICAST_IF uses the plain interface index value.
			controlErr = windows.SetsockoptInt(handle, windows.IPPROTO_IPV6, ipv6UnicastIf, ifaceIndex)
			if controlErr != nil {
				controlErr = fmt.Errorf("setsockopt IPV6_UNICAST_IF=%d: %w", ifaceIndex, controlErr)
			}
		})
		if err != nil {
			return fmt.Errorf("raw conn control: %w", err)
		}
		return controlErr
	}
	return nil
}
