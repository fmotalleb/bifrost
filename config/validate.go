package config

import (
	"fmt"
)

// Validate checks whether the parsed config is usable by the proxy.
func (c Config) Validate() error {
	if !c.Listen.IsValid() {
		return fmt.Errorf("listen must be a valid address:port")
	}

	if !c.Server.IsValid() {
		return fmt.Errorf("server must be a valid address:port")
	}

	if c.Metrics.IsValid() && c.Metrics == c.Listen {
		return fmt.Errorf("metrics must be different from listen")
	}

	if c.Cache.TTL < 0 {
		return fmt.Errorf("cache.ttl must be zero or greater")
	}

	if len(c.IFaces) == 0 {
		return fmt.Errorf("ifaces must contain at least one interface")
	}

	for name, iface := range c.IFaces {
		if name == "" {
			return fmt.Errorf("ifaces cannot contain an empty interface name")
		}

		if iface.Weight <= 0 {
			return fmt.Errorf("interface %q must have weight greater than 0", name)
		}

		if iface.SourceIP != nil {
			if iface.SourceIP.IsUnspecified() {
				return fmt.Errorf("interface %q source_ip cannot be unspecified", name)
			}
			if iface.SourceIP.IsLoopback() {
				return fmt.Errorf("interface %q source_ip cannot be loopback", name)
			}
			if !iface.SourceIP.IsGlobalUnicast() {
				return fmt.Errorf("interface %q source_ip must be a unicast address", name)
			}
		}
	}

	return nil
}
