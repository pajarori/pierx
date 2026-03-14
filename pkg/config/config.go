package config

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

type ServerConfig struct {
	Domain            string `yaml:"domain"`
	TLSCert           string `yaml:"tls_cert"`
	TLSKey            string `yaml:"tls_key"`
	TunnelPort        int    `yaml:"tunnel_port"`
	TCPMinPort        int    `yaml:"tcp_min_port"`
	TCPMaxPort        int    `yaml:"tcp_max_port"`
	TCPIdleTimeout    int    `yaml:"tcp_idle_timeout"`
	AllowAnonymousTCP bool   `yaml:"allow_anonymous_tcp"`
	HTTPPort          int    `yaml:"http_port"`
	HTTPSPort         int    `yaml:"https_port"`
	STUNPort          int    `yaml:"stun_port"`
	SignalPort        int    `yaml:"signal_port"`
	DashboardPort     int    `yaml:"dashboard_port"`
	DataDir           string `yaml:"data_dir"`
}

type ClientConfig struct {
	ServerAddr string   `yaml:"server_addr"`
	LocalPort  int      `yaml:"local_port"`
	TunnelType string   `yaml:"tunnel_type"`
	TCPAllow   []string `yaml:"tcp_allow"`
	STUNAddr   string   `yaml:"stun_addr"`
	SignalAddr string   `yaml:"signal_addr"`
	Insecure   bool     `yaml:"insecure"`
}

const DefaultDomain = "tunnel.pierx.app"

const (
	TunnelHTTP = "http"
	TunnelTCP  = "tcp"
)

const (
	DefaultTLSCert = "/etc/letsencrypt/live/tunnel.pierx.app/fullchain.pem"
	DefaultTLSKey  = "/etc/letsencrypt/live/tunnel.pierx.app/privkey.pem"
)

func DefaultServerConfig() *ServerConfig {
	return &ServerConfig{
		Domain:            DefaultDomain,
		TunnelPort:        4000,
		TCPMinPort:        20000,
		TCPMaxPort:        29999,
		TCPIdleTimeout:    15 * 60,
		AllowAnonymousTCP: true,
		HTTPPort:          80,
		HTTPSPort:         443,
		STUNPort:          3478,
		SignalPort:        7000,
		DashboardPort:     8000,
		DataDir:           ".",
	}
}

func DefaultClientConfig() *ClientConfig {
	return &ClientConfig{
		ServerAddr: DefaultDomain + ":4000",
		LocalPort:  3000,
		TunnelType: TunnelHTTP,
		STUNAddr:   DefaultDomain + ":3478",
		SignalAddr: "wss://" + DefaultDomain + ":7000",
		Insecure:   false,
	}
}

func LoadServerConfig(path string) (*ServerConfig, error) {
	cfg := DefaultServerConfig()
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}
	return cfg, nil
}

func LoadClientConfig(path string) (*ClientConfig, error) {
	cfg := DefaultClientConfig()
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}
	return cfg, nil
}

func GenerateToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate token: %w", err)
	}
	return hex.EncodeToString(b), nil
}

func ValidateServerConfig(cfg *ServerConfig) error {
	if cfg.Domain == "" {
		return fmt.Errorf("domain is required")
	}
	if cfg.TunnelPort <= 0 || cfg.TunnelPort > 65535 {
		return fmt.Errorf("invalid tunnel_port: %d", cfg.TunnelPort)
	}
	if cfg.TCPMinPort <= 0 || cfg.TCPMinPort > 65535 {
		return fmt.Errorf("invalid tcp_min_port: %d", cfg.TCPMinPort)
	}
	if cfg.TCPMaxPort <= 0 || cfg.TCPMaxPort > 65535 {
		return fmt.Errorf("invalid tcp_max_port: %d", cfg.TCPMaxPort)
	}
	if cfg.TCPMinPort > cfg.TCPMaxPort {
		return fmt.Errorf("tcp_min_port cannot be greater than tcp_max_port")
	}
	if cfg.TCPIdleTimeout < 0 {
		return fmt.Errorf("tcp_idle_timeout cannot be negative")
	}
	return nil
}

func AutoDetectTLS(cfg *ServerConfig) {
	if cfg.TLSCert != "" || cfg.TLSKey != "" {
		return
	}
	certPath := fmt.Sprintf("/etc/letsencrypt/live/%s/fullchain.pem", cfg.Domain)
	keyPath := fmt.Sprintf("/etc/letsencrypt/live/%s/privkey.pem", cfg.Domain)
	if fileExists(certPath) && fileExists(keyPath) {
		cfg.TLSCert = certPath
		cfg.TLSKey = keyPath
	}
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
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
