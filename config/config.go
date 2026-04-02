package config

import "net/netip"

// Config contains the runtime settings for bifrost.
type Config struct {
	Listen netip.AddrPort   `mapstructure:"listen"`
	Server netip.AddrPort   `mapstructure:"server"`
	IFaces map[string]Iface `mapstructure:"ifaces"`
}

// Iface defines configuration for a network interface.
type Iface struct {
	Weight int `mapstructure:"weight"`
}
