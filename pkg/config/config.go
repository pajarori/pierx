package config

import "fmt"

type ClientConfig struct {
	ServerAddr string   `yaml:"server_addr"`
	LocalPort  int      `yaml:"local_port"`
	TunnelType string   `yaml:"tunnel_type"`
	TCPAllow   []string `yaml:"tcp_allow"`
	STUNAddr   string   `yaml:"stun_addr"`
	SignalAddr string   `yaml:"signal_addr"`
}

const DefaultDomain = "tunnel.pierx.app"

const (
	TunnelHTTP = "http"
	TunnelTCP  = "tcp"
)

func DefaultClientConfig() *ClientConfig {
	return &ClientConfig{
		ServerAddr: DefaultDomain + ":4000",
		LocalPort:  3000,
		TunnelType: TunnelHTTP,
		STUNAddr:   DefaultDomain + ":3478",
		SignalAddr: "ws://" + DefaultDomain + ":7000",
	}
}

func ValidateClientConfig(cfg *ClientConfig) error {
	if cfg.ServerAddr == "" {
		return fmt.Errorf("server_addr is required")
	}
	if cfg.LocalPort <= 0 || cfg.LocalPort > 65535 {
		return fmt.Errorf("invalid local_port: %d", cfg.LocalPort)
	}
	if cfg.TunnelType == "" {
		cfg.TunnelType = TunnelHTTP
	}
	if cfg.TunnelType != TunnelHTTP && cfg.TunnelType != TunnelTCP {
		return fmt.Errorf("invalid tunnel_type: %s", cfg.TunnelType)
	}
	return nil
}
