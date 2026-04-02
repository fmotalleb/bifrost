package proxy

import (
	"fmt"
	"net"
	"strings"
)

// ResolveBindIP resolves a usable source IP for the given interface.
func ResolveBindIP(interfaceName string, preferIPv4 bool) (net.IP, error) {
	iface, err := resolveInterfaceByName(interfaceName)
	if err != nil {
		return nil, err
	}
	return resolveBindIPFromInterface(iface, interfaceName, preferIPv4)
}

// ResolveBindIPByIndex resolves a usable source IP for the given interface index.
func ResolveBindIPByIndex(interfaceIndex int, preferIPv4 bool) (net.IP, error) {
	iface, err := net.InterfaceByIndex(interfaceIndex)
	if err != nil {
		return nil, fmt.Errorf("find interface by index %d: %w", interfaceIndex, err)
	}

	return resolveBindIPFromInterface(iface, iface.Name, preferIPv4)
}

func resolveBindIPFromInterface(iface *net.Interface, interfaceName string, preferIPv4 bool) (net.IP, error) {
	if iface == nil {
		return nil, fmt.Errorf("interface %q is nil", interfaceName)
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

// ResolveInterfaceName resolves a configured interface name to the exact OS interface name.
func ResolveInterfaceName(interfaceName string) (string, error) {
	iface, err := resolveInterfaceByName(interfaceName)
	if err != nil {
		return "", err
	}

	return iface.Name, nil
}

// ResolveInterface resolves a configured interface name to an OS interface.
func ResolveInterface(interfaceName string) (*net.Interface, error) {
	return resolveInterfaceByName(interfaceName)
}

func resolveInterfaceByName(interfaceName string) (*net.Interface, error) {
	target := normalizeIfaceName(interfaceName)
	if target == "" {
		return nil, fmt.Errorf("interface name cannot be empty")
	}

	iface, err := net.InterfaceByName(target)
	if err == nil {
		return iface, nil
	}

	ifaces, listErr := net.Interfaces()
	if listErr != nil {
		return nil, fmt.Errorf("find interface %q: %w", interfaceName, err)
	}

	for idx := range ifaces {
		if strings.EqualFold(ifaces[idx].Name, target) {
			return &ifaces[idx], nil
		}
	}

	availableNames := make([]string, 0, len(ifaces))
	for _, available := range ifaces {
		availableNames = append(availableNames, available.Name)
	}

	return nil, fmt.Errorf(
		"find interface %q: %w (available: %s)",
		interfaceName,
		err,
		strings.Join(availableNames, ", "),
	)
}

func normalizeIfaceName(interfaceName string) string {
	parts := strings.Fields(interfaceName)
	return strings.Join(parts, " ")
}
