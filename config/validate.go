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
	}

	return nil
}
