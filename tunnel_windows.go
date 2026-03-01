//go:build windows

package main

import (
	"bufio"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"
	"golang.org/x/net/proxy"
)

type ActiveTunnel struct {
	Name      string
	LocalPort int

	listener net.Listener
	client   *ssh.Client
	target   string

	closeOnce sync.Once
}

func (t *ActiveTunnel) Close() {
	t.closeOnce.Do(func() {
		if t.listener != nil {
			_ = t.listener.Close()
		}
		if t.client != nil {
			_ = t.client.Close()
		}
	})
}

func StartTunnelWithFallback(tunnels []*TunnelConfig) (*ActiveTunnel, *TunnelConfig, error) {
	if len(tunnels) == 0 {
		return nil, nil, errors.New("没有可用 SSH 隧道配置")
	}

	candidates := make([]*TunnelConfig, 0, len(tunnels))
	for _, tunnel := range tunnels {
		candidates = append(candidates, cloneTunnelConfig(tunnel))
	}

	sort.SliceStable(candidates, func(i, j int) bool {
		return candidates[i].Priority < candidates[j].Priority
	})

	var attempts []string
	for _, tunnel := range candidates {
		active, err := startSingleTunnel(tunnel)
		if err != nil {
			attempts = append(attempts, fmt.Sprintf("%s: %v", displayTunnelName(tunnel), err))
			continue
		}
		return active, tunnel, nil
	}

	return nil, nil, fmt.Errorf("全部隧道连接失败:\n%s", strings.Join(attempts, "\n"))
}

func startSingleTunnel(tunnel *TunnelConfig) (*ActiveTunnel, error) {
	normalizeTunnelConfig(tunnel)

	client, err := dialSSHClient(tunnel)
	if err != nil {
		return nil, err
	}

	target := net.JoinHostPort(tunnel.RemoteHost, strconv.Itoa(tunnel.RemotePort))
	testConn, err := client.Dial("tcp", target)
	if err != nil {
		_ = client.Close()
		return nil, fmt.Errorf("远端 RDP 目标不可达(%s): %w", target, err)
	}
	_ = testConn.Close()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		_ = client.Close()
		return nil, fmt.Errorf("打开本地端口失败: %w", err)
	}

	port := listener.Addr().(*net.TCPAddr).Port
	active := &ActiveTunnel{
		Name:      displayTunnelName(tunnel),
		LocalPort: port,
		listener:  listener,
		client:    client,
		target:    target,
	}

	go active.acceptLoop()
	return active, nil
}

func (t *ActiveTunnel) acceptLoop() {
	for {
		localConn, err := t.listener.Accept()
		if err != nil {
			if errors.Is(err, net.ErrClosed) {
				return
			}
			continue
		}

		go t.forwardConn(localConn)
	}
}

func (t *ActiveTunnel) forwardConn(localConn net.Conn) {
	remoteConn, err := t.client.Dial("tcp", t.target)
	if err != nil {
		_ = localConn.Close()
		return
	}

	copyFinished := make(chan struct{}, 2)
	go func() {
		_, _ = io.Copy(remoteConn, localConn)
		copyFinished <- struct{}{}
	}()
	go func() {
		_, _ = io.Copy(localConn, remoteConn)
		copyFinished <- struct{}{}
	}()

	<-copyFinished
	_ = localConn.Close()
	_ = remoteConn.Close()
}

func dialSSHClient(tunnel *TunnelConfig) (*ssh.Client, error) {
	authMethod, err := buildSSHAuthMethod(tunnel)
	if err != nil {
		return nil, err
	}

	timeout := time.Duration(tunnel.ConnectTimeoutSec) * time.Second
	if timeout <= 0 {
		timeout = time.Duration(defaultTimeoutSec) * time.Second
	}

	sshAddr := net.JoinHostPort(tunnel.SSHHost, strconv.Itoa(tunnel.SSHPort))
	conn, err := dialForSSH(sshAddr, tunnel.Proxy, timeout)
	if err != nil {
		return nil, err
	}

	cfg := &ssh.ClientConfig{
		User:            tunnel.SSHUser,
		Auth:            []ssh.AuthMethod{authMethod},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         timeout,
	}

	c, chans, reqs, err := ssh.NewClientConn(conn, sshAddr, cfg)
	if err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("SSH 握手失败: %w", err)
	}
	return ssh.NewClient(c, chans, reqs), nil
}

func buildSSHAuthMethod(tunnel *TunnelConfig) (ssh.AuthMethod, error) {
	if tunnel.SSHHost == "" {
		return nil, errors.New("SSH Host 不能为空")
	}
	if tunnel.SSHUser == "" {
		return nil, errors.New("SSH 用户名不能为空")
	}

	switch strings.ToLower(tunnel.AuthType) {
	case "key", "private_key":
		if tunnel.PrivateKeyPath == "" {
			return nil, errors.New("私钥路径不能为空")
		}
		keyData, err := os.ReadFile(tunnel.PrivateKeyPath)
		if err != nil {
			return nil, fmt.Errorf("读取私钥失败: %w", err)
		}

		var signer ssh.Signer
		if tunnel.PrivateKeyPassphrase != "" {
			signer, err = ssh.ParsePrivateKeyWithPassphrase(keyData, []byte(tunnel.PrivateKeyPassphrase))
		} else {
			signer, err = ssh.ParsePrivateKey(keyData)
		}
		if err != nil {
			return nil, fmt.Errorf("解析私钥失败: %w", err)
		}
		return ssh.PublicKeys(signer), nil
	default:
		if tunnel.Password == "" {
			return nil, errors.New("SSH 密码不能为空")
		}
		return ssh.Password(tunnel.Password), nil
	}
}

func dialForSSH(sshAddr string, cfg ProxyConfig, timeout time.Duration) (net.Conn, error) {
	switch strings.ToLower(cfg.Type) {
	case "", "none", "direct":
		conn, err := net.DialTimeout("tcp", sshAddr, timeout)
		if err != nil {
			return nil, fmt.Errorf("SSH 直连失败: %w", err)
		}
		return conn, nil
	case "socks5":
		if cfg.Address == "" {
			return nil, errors.New("SOCKS5 代理地址不能为空")
		}

		var auth *proxy.Auth
		if cfg.Username != "" {
			auth = &proxy.Auth{
				User:     cfg.Username,
				Password: cfg.Password,
			}
		}

		dialer, err := proxy.SOCKS5(
			"tcp",
			cfg.Address,
			auth,
			&net.Dialer{Timeout: timeout},
		)
		if err != nil {
			return nil, fmt.Errorf("初始化 SOCKS5 代理失败: %w", err)
		}

		conn, err := dialer.Dial("tcp", sshAddr)
		if err != nil {
			return nil, fmt.Errorf("通过 SOCKS5 连接 SSH 失败: %w", err)
		}
		return conn, nil
	case "http":
		if cfg.Address == "" {
			return nil, errors.New("HTTP 代理地址不能为空")
		}
		return dialHTTPProxyConnect(cfg, sshAddr, timeout)
	default:
		return nil, fmt.Errorf("不支持的代理类型: %s", cfg.Type)
	}
}

func dialHTTPProxyConnect(cfg ProxyConfig, sshAddr string, timeout time.Duration) (net.Conn, error) {
	conn, err := net.DialTimeout("tcp", cfg.Address, timeout)
	if err != nil {
		return nil, fmt.Errorf("连接 HTTP 代理失败: %w", err)
	}

	if err := conn.SetDeadline(time.Now().Add(timeout)); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("设置 HTTP 代理超时失败: %w", err)
	}

	var b strings.Builder
	b.WriteString(fmt.Sprintf("CONNECT %s HTTP/1.1\r\n", sshAddr))
	b.WriteString(fmt.Sprintf("Host: %s\r\n", sshAddr))
	b.WriteString("Proxy-Connection: Keep-Alive\r\n")
	if cfg.Username != "" {
		token := base64.StdEncoding.EncodeToString([]byte(cfg.Username + ":" + cfg.Password))
		b.WriteString("Proxy-Authorization: Basic " + token + "\r\n")
	}
	b.WriteString("\r\n")

	if _, err := io.WriteString(conn, b.String()); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("发送 HTTP CONNECT 失败: %w", err)
	}

	resp, err := http.ReadResponse(bufio.NewReader(conn), &http.Request{Method: http.MethodConnect})
	if err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("读取 HTTP CONNECT 响应失败: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		_ = resp.Body.Close()
		_ = conn.Close()
		return nil, fmt.Errorf("HTTP CONNECT 失败: %s", resp.Status)
	}

	if err := conn.SetDeadline(time.Time{}); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("清理 HTTP 代理超时失败: %w", err)
	}

	return conn, nil
}

func displayTunnelName(tunnel *TunnelConfig) string {
	if tunnel == nil {
		return "未命名隧道"
	}
	if tunnel.Name != "" {
		return tunnel.Name
	}
	return fmt.Sprintf("%s@%s:%d", tunnel.SSHUser, tunnel.SSHHost, tunnel.SSHPort)
}
