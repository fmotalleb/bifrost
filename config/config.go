package config

type Config struct {
	IFaces map[string]Iface `mapstructure:"ifaces"`
}

type Iface struct {
	Weight int `mapstructure:"weight"`
}
