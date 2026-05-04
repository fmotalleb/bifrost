//go:build linux

package proxy

import (
	"errors"
	"fmt"
	"net"
	"syscall"

	"golang.org/x/sys/unix"
)

func configureDialerInterfacePinning(dialer *net.Dialer, ifaceName string, ifaceIndex int, bindIP net.IP) error {
	if dialer == nil {
		return nil
	}
	if ifaceName == "" && ifaceIndex <= 0 {
		return nil
	}
	if bindIP == nil {
		return nil
	}

	name := ifaceName
	if name == "" {
		iface, err := net.InterfaceByIndex(ifaceIndex)
		if err != nil {
			return fmt.Errorf("find interface by index %d: %w", ifaceIndex, err)
		}
		name = iface.Name
	}

	dialer.Control = func(_, _ string, rawConn syscall.RawConn) error {
		var controlErr error
		err := rawConn.Control(func(fd uintptr) {
			controlErr = unix.SetsockoptString(int(fd), unix.SOL_SOCKET, unix.SO_BINDTODEVICE, name)
			if controlErr != nil {
				if errors.Is(controlErr, unix.EPERM) || errors.Is(controlErr, unix.EACCES) {
					controlErr = fmt.Errorf("bind to device %q requires CAP_NET_RAW/CAP_NET_ADMIN: %w", name, controlErr)
					return
				}
				controlErr = fmt.Errorf("setsockopt SO_BINDTODEVICE %q: %w", name, controlErr)
			}
		})
		if err != nil {
			return fmt.Errorf("raw conn control: %w", err)
		}
		return controlErr
	}
	return nil
}
