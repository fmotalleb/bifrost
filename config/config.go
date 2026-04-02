package config

import (
	"net"
	"net/netip"
	"time"
)

// Config contains the runtime settings for bifrost.
type Config struct {
	Listen netip.AddrPort   `mapstructure:"listen"`
	Server netip.AddrPort   `mapstructure:"server"`
	Cache  CacheConfig      `mapstructure:"cache"`
	IFaces map[string]Iface `mapstructure:"ifaces"`
}

// CacheConfig controls source IP lookup caching behavior.
type CacheConfig struct {
	TTL      time.Duration `mapstructure:"ttl"`
	Prefetch bool          `mapstructure:"prefetch"`
}

// Iface defines configuration for a network interface.
type Iface struct {
	Weight   int    `mapstructure:"weight"`
	SourceIP net.IP `mapstructure:"source_ip"`
}
