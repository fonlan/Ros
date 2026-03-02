//go:build windows

package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

const (
	appName             = "Ros"
	defaultSSHPort      = 22
	defaultRDPPort      = 3389
	defaultTimeoutSec   = 12
	defaultDesktopWidth = 1920
	defaultDesktopHigh  = 1080
)

type AppConfig struct {
	Servers []*ServerConfig `json:"servers"`
}

type ServerConfig struct {
	ID      string          `json:"id"`
	Name    string          `json:"name"`
	RDP     RDPConfig       `json:"rdp"`
	Tunnels []*TunnelConfig `json:"tunnels"`
}

type RDPConfig struct {
	Username           string `json:"username"`
	Password           string `json:"password"`
	Domain             string `json:"domain"`
	AdaptiveResolution bool   `json:"adaptive_resolution"`
	DesktopWidth       int    `json:"desktop_width"`
	DesktopHeight      int    `json:"desktop_height"`
	RedirectDisks      bool   `json:"redirect_disks"`
	RedirectSound      bool   `json:"redirect_sound"`
	RedirectClipboard  bool   `json:"redirect_clipboard"`
}

type TunnelConfig struct {
	Name                 string      `json:"name"`
	Priority             int         `json:"priority"`
	SSHHost              string      `json:"ssh_host"`
	SSHPort              int         `json:"ssh_port"`
	SSHUser              string      `json:"ssh_user"`
	AuthType             string      `json:"auth_type"`
	Password             string      `json:"password"`
	PrivateKeyPath       string      `json:"private_key_path"`
	PrivateKeyPassphrase string      `json:"private_key_passphrase"`
	JumpHost             JumpConfig  `json:"jump_host"`
	Proxy                ProxyConfig `json:"proxy"`
	RemoteHost           string      `json:"remote_host"`
	RemotePort           int         `json:"remote_port"`
	ConnectTimeoutSec    int         `json:"connect_timeout_sec"`
}

type JumpConfig struct {
	SSHHost              string `json:"ssh_host"`
	SSHPort              int    `json:"ssh_port"`
	SSHUser              string `json:"ssh_user"`
	AuthType             string `json:"auth_type"`
	Password             string `json:"password"`
	PrivateKeyPath       string `json:"private_key_path"`
	PrivateKeyPassphrase string `json:"private_key_passphrase"`
}

type ProxyConfig struct {
	Type     string `json:"type"`
	Address  string `json:"address"`
	Username string `json:"username"`
	Password string `json:"password"`
}

func loadConfig() (string, *AppConfig, error) {
	path, err := configFilePath()
	if err != nil {
		return "", nil, err
	}

	cfg := &AppConfig{Servers: []*ServerConfig{}}
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		return path, cfg, nil
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		return "", nil, fmt.Errorf("read config: %w", err)
	}
	if len(raw) == 0 {
		return path, cfg, nil
	}

	if err := json.Unmarshal(raw, cfg); err != nil {
		return "", nil, fmt.Errorf("parse config: %w", err)
	}

	normalizeConfig(cfg)
	return path, cfg, nil
}

func saveConfig(path string, cfg *AppConfig) error {
	normalizeConfig(cfg)

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}

	raw, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}

	tmpPath := path + ".tmp"
	if err := os.WriteFile(tmpPath, raw, 0o600); err != nil {
		return fmt.Errorf("write temp config: %w", err)
	}

	_ = os.Remove(path)
	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("replace config: %w", err)
	}

	return nil
}

func configFilePath() (string, error) {
	baseDir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("resolve user config dir: %w", err)
	}
	return filepath.Join(baseDir, appName, "config.json"), nil
}

func normalizeConfig(cfg *AppConfig) {
	if cfg.Servers == nil {
		cfg.Servers = []*ServerConfig{}
	}

	for _, server := range cfg.Servers {
		normalizeServerConfig(server)
	}
}

func normalizeServerConfig(server *ServerConfig) {
	if server == nil {
		return
	}

	if server.ID == "" {
		server.ID = generateID()
	}
	if server.Name == "" {
		server.Name = "新服务器"
	}

	if server.RDP.DesktopWidth <= 0 {
		server.RDP.DesktopWidth = defaultDesktopWidth
	}
	if server.RDP.DesktopHeight <= 0 {
		server.RDP.DesktopHeight = defaultDesktopHigh
	}

	if server.Tunnels == nil {
		server.Tunnels = []*TunnelConfig{}
	}

	for _, tunnel := range server.Tunnels {
		normalizeTunnelConfig(tunnel)
	}
	normalizeTunnelPriorities(server.Tunnels)
}

func normalizeTunnelConfig(tunnel *TunnelConfig) {
	if tunnel == nil {
		return
	}

	if tunnel.Name == "" {
		tunnel.Name = "隧道"
	}
	if tunnel.SSHPort <= 0 {
		tunnel.SSHPort = defaultSSHPort
	}
	if tunnel.AuthType == "" {
		tunnel.AuthType = "password"
	}
	if tunnel.RemoteHost == "" {
		tunnel.RemoteHost = "127.0.0.1"
	}
	if tunnel.RemotePort <= 0 {
		tunnel.RemotePort = defaultRDPPort
	}
	if tunnel.ConnectTimeoutSec <= 0 {
		tunnel.ConnectTimeoutSec = defaultTimeoutSec
	}
	if tunnel.Proxy.Type == "" {
		tunnel.Proxy.Type = "none"
	}
	if tunnel.JumpHost.SSHHost == "" {
		tunnel.JumpHost = JumpConfig{}
	} else {
		if tunnel.JumpHost.SSHPort <= 0 {
			tunnel.JumpHost.SSHPort = defaultSSHPort
		}
		if tunnel.JumpHost.AuthType == "" {
			tunnel.JumpHost.AuthType = "password"
		}
	}
}

func normalizeTunnelPriorities(tunnels []*TunnelConfig) {
	sort.SliceStable(tunnels, func(i, j int) bool {
		return tunnels[i].Priority < tunnels[j].Priority
	})
	for i, tunnel := range tunnels {
		tunnel.Priority = i + 1
	}
}

func sortedTunnelCopy(tunnels []*TunnelConfig) []*TunnelConfig {
	out := make([]*TunnelConfig, 0, len(tunnels))
	for _, tunnel := range tunnels {
		out = append(out, cloneTunnelConfig(tunnel))
	}
	normalizeTunnelPriorities(out)
	return out
}

func cloneServerConfig(src *ServerConfig) *ServerConfig {
	if src == nil {
		return &ServerConfig{
			ID:      generateID(),
			Name:    "新服务器",
			Tunnels: []*TunnelConfig{},
			RDP: RDPConfig{
				AdaptiveResolution: true,
				DesktopWidth:       defaultDesktopWidth,
				DesktopHeight:      defaultDesktopHigh,
				RedirectDisks:      false,
				RedirectSound:      true,
				RedirectClipboard:  true,
			},
		}
	}

	cloned := *src
	cloned.Tunnels = make([]*TunnelConfig, 0, len(src.Tunnels))
	for _, tunnel := range src.Tunnels {
		cloned.Tunnels = append(cloned.Tunnels, cloneTunnelConfig(tunnel))
	}
	normalizeServerConfig(&cloned)
	return &cloned
}

func cloneTunnelConfig(src *TunnelConfig) *TunnelConfig {
	if src == nil {
		tunnel := &TunnelConfig{
			Name:              "隧道",
			Priority:          1,
			SSHPort:           defaultSSHPort,
			AuthType:          "password",
			RemoteHost:        "127.0.0.1",
			RemotePort:        defaultRDPPort,
			ConnectTimeoutSec: defaultTimeoutSec,
			Proxy: ProxyConfig{
				Type: "none",
			},
		}
		return tunnel
	}

	cloned := *src
	normalizeTunnelConfig(&cloned)
	return &cloned
}

func generateID() string {
	buf := make([]byte, 8)
	if _, err := rand.Read(buf); err != nil {
		return "fallback-id"
	}
	return hex.EncodeToString(buf)
}
