//go:build darwin

package proxy

import (
	"fmt"
	"net"
	"syscall"

	"golang.org/x/sys/unix"
)

func configureDialerInterfacePinning(dialer *net.Dialer, _ string, ifaceIndex int, bindIP net.IP) error {
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
			if boundIPv4 {
				controlErr = unix.SetsockoptInt(int(fd), unix.IPPROTO_IP, unix.IP_BOUND_IF, ifaceIndex)
				if controlErr != nil {
					controlErr = fmt.Errorf("setsockopt IP_BOUND_IF=%d: %w", ifaceIndex, controlErr)
				}
				return
			}

			controlErr = unix.SetsockoptInt(int(fd), unix.IPPROTO_IPV6, unix.IPV6_BOUND_IF, ifaceIndex)
			if controlErr != nil {
				controlErr = fmt.Errorf("setsockopt IPV6_BOUND_IF=%d: %w", ifaceIndex, controlErr)
			}
		})
		if err != nil {
			return fmt.Errorf("raw conn control: %w", err)
		}
		return controlErr
	}
	return nil
}
