package proxy

import (
	"fmt"
	"net"
)

// ResolveBindIP resolves a usable source IP for the given interface.
func ResolveBindIP(interfaceName string, preferIPv4 bool) (net.IP, error) {
	iface, err := net.InterfaceByName(interfaceName)
	if err != nil {
		return nil, fmt.Errorf("find interface %q: %w", interfaceName, err)
	}

	addrs, err := iface.Addrs()
	if err != nil {
		return nil, fmt.Errorf("list addresses for %q: %w", interfaceName, err)
	}

	matchedFamily := pickAddress(addrs, preferIPv4)
	if matchedFamily != nil {
		return matchedFamily, nil
	}

	fallback := pickAnyAddress(addrs)
	if fallback != nil {
		return fallback, nil
	}

	return nil, fmt.Errorf("interface %q has no usable IP address", interfaceName)
}

func pickAddress(addrs []net.Addr, preferIPv4 bool) net.IP {
	for _, addr := range addrs {
		ip := ipFromAddr(addr)
		if ip == nil || ip.IsLoopback() || !ip.IsGlobalUnicast() {
			continue
		}

		if preferIPv4 && ip.To4() != nil {
			return ip.To4()
		}

		if !preferIPv4 && ip.To4() == nil {
			return ip
		}
	}

	return nil
}

func pickAnyAddress(addrs []net.Addr) net.IP {
	for _, addr := range addrs {
		ip := ipFromAddr(addr)
		if ip == nil || ip.IsLoopback() || !ip.IsGlobalUnicast() {
			continue
		}

		if ip4 := ip.To4(); ip4 != nil {
			return ip4
		}

		return ip
	}

	return nil
}

func ipFromAddr(addr net.Addr) net.IP {
	switch value := addr.(type) {
	case *net.IPNet:
		return value.IP
	case *net.IPAddr:
		return value.IP
	default:
		return nil
	}
}
