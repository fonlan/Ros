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
	jump     *ssh.Client
	target   string

	candidates   []*TunnelConfig
	currentIndex int
	statusFn     func(string)

	stateMu     sync.RWMutex
	reconnectMu sync.Mutex
	closeOnce   sync.Once
	closedCh    chan struct{}
}

func (t *ActiveTunnel) Close() {
	t.closeOnce.Do(func() {
		if t.closedCh != nil {
			close(t.closedCh)
		}
		if t.listener != nil {
			_ = t.listener.Close()
		}

		t.reconnectMu.Lock()
		t.stateMu.Lock()
		client := t.client
		jump := t.jump
		t.client = nil
		t.jump = nil
		t.stateMu.Unlock()
		t.reconnectMu.Unlock()

		if client != nil {
			_ = client.Close()
		}
		if jump != nil && jump != client {
			_ = jump.Close()
		}
	})
}

func (t *ActiveTunnel) SetStatusHandler(fn func(string)) {
	t.stateMu.Lock()
	t.statusFn = fn
	t.stateMu.Unlock()
}

func (t *ActiveTunnel) reportStatus(msg string) {
	if strings.TrimSpace(msg) == "" {
		return
	}

	t.stateMu.RLock()
	fn := t.statusFn
	t.stateMu.RUnlock()
	if fn != nil {
		fn(msg)
	}
}

func (t *ActiveTunnel) isClosed() bool {
	if t.closedCh == nil {
		return true
	}
	select {
	case <-t.closedCh:
		return true
	default:
		return false
	}
}

func StartTunnelWithFallback(tunnels []*TunnelConfig) (*ActiveTunnel, *TunnelConfig, error) {
	candidates := make([]*TunnelConfig, 0, len(tunnels))
	for _, tunnel := range tunnels {
		if tunnel == nil || !isTunnelEnabled(tunnel) {
			continue
		}
		candidates = append(candidates, cloneTunnelConfig(tunnel))
	}
	if len(candidates) == 0 {
		return nil, nil, errors.New("没有已启用的 SSH 隧道配置")
	}

	sort.SliceStable(candidates, func(i, j int) bool {
		return candidates[i].Priority < candidates[j].Priority
	})

	var attempts []string
	for i, tunnel := range candidates {
		client, jump, target, err := establishTunnelClient(tunnel)
		if err != nil {
			attempts = append(attempts, fmt.Sprintf("%s: %v", displayTunnelName(tunnel), err))
			continue
		}

		listener, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			closeSSHClients(client, jump)
			return nil, nil, fmt.Errorf("打开本地端口失败: %w", err)
		}

		port := listener.Addr().(*net.TCPAddr).Port
		active := &ActiveTunnel{
			Name:         displayTunnelName(tunnel),
			LocalPort:    port,
			listener:     listener,
			client:       client,
			jump:         jump,
			target:       target,
			candidates:   candidates,
			currentIndex: i,
			closedCh:     make(chan struct{}),
		}

		go active.acceptLoop()
		return active, cloneTunnelConfig(tunnel), nil
	}

	return nil, nil, fmt.Errorf("全部隧道连接失败:\n%s", strings.Join(attempts, "\n"))
}

func establishTunnelClient(tunnel *TunnelConfig) (*ssh.Client, *ssh.Client, string, error) {
	normalizeTunnelConfig(tunnel)

	client, jump, err := dialSSHClient(tunnel)
	if err != nil {
		return nil, nil, "", err
	}

	target := net.JoinHostPort(tunnel.RemoteHost, strconv.Itoa(tunnel.RemotePort))
	testConn, err := client.Dial("tcp", target)
	if err != nil {
		closeSSHClients(client, jump)
		return nil, nil, "", fmt.Errorf("远端 RDP 目标不可达(%s): %w", target, err)
	}
	_ = testConn.Close()
	return client, jump, target, nil
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
	remoteConn, err := t.dialTargetWithRecovery()
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

func (t *ActiveTunnel) dialTargetWithRecovery() (net.Conn, error) {
	client, _, target, _, _ := t.snapshot()
	if client == nil {
		if err := t.recoverTunnel(nil, errors.New("SSH 客户端已断开")); err != nil {
			return nil, err
		}

		client, _, target, _, _ = t.snapshot()
		if client == nil {
			return nil, errors.New("SSH 隧道不可用")
		}
	}

	remoteConn, err := client.Dial("tcp", target)
	if err == nil {
		return remoteConn, nil
	}

	if recoverErr := t.recoverTunnel(client, fmt.Errorf("隧道连接中断: %w", err)); recoverErr != nil {
		return nil, recoverErr
	}

	client, _, target, _, _ = t.snapshot()
	if client == nil {
		return nil, errors.New("SSH 隧道不可用")
	}

	remoteConn, err = client.Dial("tcp", target)
	if err != nil {
		return nil, fmt.Errorf("自动重连后仍无法连接远端目标: %w", err)
	}
	return remoteConn, nil
}

func (t *ActiveTunnel) snapshot() (*ssh.Client, *ssh.Client, string, int, string) {
	t.stateMu.RLock()
	defer t.stateMu.RUnlock()
	return t.client, t.jump, t.target, t.currentIndex, t.Name
}

func (t *ActiveTunnel) recoverTunnel(failedClient *ssh.Client, cause error) error {
	if t.isClosed() {
		return net.ErrClosed
	}

	t.reconnectMu.Lock()
	defer t.reconnectMu.Unlock()

	if t.isClosed() {
		return net.ErrClosed
	}

	currentClient, currentJump, _, currentIndex, previousName := t.snapshot()
	if failedClient != nil && currentClient != failedClient {
		return nil
	}

	startIndex := currentIndex
	if startIndex < 0 || startIndex >= len(t.candidates) {
		startIndex = 0
	}

	var attempts []string
	for i := startIndex; i < len(t.candidates); i++ {
		tunnel := cloneTunnelConfig(t.candidates[i])

		client, jump, target, err := establishTunnelClient(tunnel)
		if err != nil {
			attempts = append(attempts, fmt.Sprintf("%s: %v", displayTunnelName(tunnel), err))
			continue
		}

		t.stateMu.Lock()
		oldClient := t.client
		oldJump := t.jump
		oldName := t.Name

		t.client = client
		t.jump = jump
		t.target = target
		t.currentIndex = i
		t.Name = displayTunnelName(tunnel)
		newName := t.Name
		t.stateMu.Unlock()

		if oldClient != client || oldJump != jump {
			closeSSHClients(oldClient, oldJump)
		}

		if i == startIndex {
			t.reportStatus(fmt.Sprintf("SSH 隧道已自动重连: %s", newName))
		} else {
			fromName := oldName
			if fromName == "" {
				fromName = previousName
			}
			if fromName == "" {
				fromName = "当前隧道"
			}
			t.reportStatus(fmt.Sprintf("SSH 隧道已自动切换: %s -> %s", fromName, newName))
		}
		return nil
	}

	closeSSHClients(currentClient, currentJump)
	t.stateMu.Lock()
	t.client = nil
	t.jump = nil
	t.stateMu.Unlock()

	summary := strings.Join(attempts, "; ")
	if summary == "" && cause != nil {
		summary = cause.Error()
	}
	err := fmt.Errorf("自动重连失败，已尝试当前及后续优先级隧道: %s", summary)
	t.reportStatus(err.Error())
	return err
}

func hasJumpHost(tunnel *TunnelConfig) bool {
	return tunnel != nil && strings.TrimSpace(tunnel.JumpHost.SSHHost) != ""
}

func dialSSHClient(tunnel *TunnelConfig) (*ssh.Client, *ssh.Client, error) {
	targetAuth, err := buildSSHAuthMethodByFields(
		"SSH",
		tunnel.SSHHost,
		tunnel.SSHUser,
		tunnel.AuthType,
		tunnel.Password,
		tunnel.PrivateKeyPath,
		tunnel.PrivateKeyPassphrase,
	)
	if err != nil {
		return nil, nil, err
	}

	timeout := time.Duration(tunnel.ConnectTimeoutSec) * time.Second
	if timeout <= 0 {
		timeout = time.Duration(defaultTimeoutSec) * time.Second
	}

	targetAddr := net.JoinHostPort(tunnel.SSHHost, strconv.Itoa(tunnel.SSHPort))
	if !hasJumpHost(tunnel) {
		conn, err := dialForSSH(targetAddr, tunnel.Proxy, timeout)
		if err != nil {
			return nil, nil, err
		}
		targetClient, err := dialSSHOnConn(conn, targetAddr, tunnel.SSHUser, targetAuth, timeout, "SSH 握手失败")
		if err != nil {
			return nil, nil, err
		}
		return targetClient, nil, nil
	}

	jump := tunnel.JumpHost
	jumpAuth, err := buildSSHAuthMethodByFields(
		"跳板机",
		jump.SSHHost,
		jump.SSHUser,
		jump.AuthType,
		jump.Password,
		jump.PrivateKeyPath,
		jump.PrivateKeyPassphrase,
	)
	if err != nil {
		return nil, nil, err
	}

	jumpAddr := net.JoinHostPort(jump.SSHHost, strconv.Itoa(jump.SSHPort))
	jumpConn, err := dialForSSH(jumpAddr, tunnel.Proxy, timeout)
	if err != nil {
		return nil, nil, fmt.Errorf("连接跳板机失败: %w", err)
	}

	jumpClient, err := dialSSHOnConn(jumpConn, jumpAddr, jump.SSHUser, jumpAuth, timeout, "跳板机 SSH 握手失败")
	if err != nil {
		return nil, nil, err
	}

	targetConn, err := jumpClient.Dial("tcp", targetAddr)
	if err != nil {
		_ = jumpClient.Close()
		return nil, nil, fmt.Errorf("通过跳板机连接目标 SSH 失败: %w", err)
	}

	targetClient, err := dialSSHOnConn(targetConn, targetAddr, tunnel.SSHUser, targetAuth, timeout, "目标 SSH 握手失败")
	if err != nil {
		_ = jumpClient.Close()
		return nil, nil, err
	}
	return targetClient, jumpClient, nil
}

func dialSSHOnConn(conn net.Conn, sshAddr, user string, authMethod ssh.AuthMethod, timeout time.Duration, handshakeErrPrefix string) (*ssh.Client, error) {
	cfg := &ssh.ClientConfig{
		User:            user,
		Auth:            []ssh.AuthMethod{authMethod},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         timeout,
	}

	c, chans, reqs, err := ssh.NewClientConn(conn, sshAddr, cfg)
	if err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("%s: %w", handshakeErrPrefix, err)
	}
	return ssh.NewClient(c, chans, reqs), nil
}

func buildSSHAuthMethodByFields(role, host, user, authType, password, privateKeyPath, privateKeyPassphrase string) (ssh.AuthMethod, error) {
	if strings.TrimSpace(host) == "" {
		return nil, fmt.Errorf("%s Host 不能为空", role)
	}
	if strings.TrimSpace(user) == "" {
		return nil, fmt.Errorf("%s 用户名不能为空", role)
	}

	switch strings.ToLower(authType) {
	case "key", "private_key":
		if strings.TrimSpace(privateKeyPath) == "" {
			return nil, fmt.Errorf("%s 私钥路径不能为空", role)
		}
		keyData, err := os.ReadFile(privateKeyPath)
		if err != nil {
			return nil, fmt.Errorf("读取%s私钥失败: %w", role, err)
		}

		var signer ssh.Signer
		if privateKeyPassphrase != "" {
			signer, err = ssh.ParsePrivateKeyWithPassphrase(keyData, []byte(privateKeyPassphrase))
		} else {
			signer, err = ssh.ParsePrivateKey(keyData)
		}
		if err != nil {
			return nil, fmt.Errorf("解析%s私钥失败: %w", role, err)
		}
		return ssh.PublicKeys(signer), nil
	default:
		if password == "" {
			return nil, fmt.Errorf("%s 密码不能为空", role)
		}
		return ssh.Password(password), nil
	}
}

func closeSSHClients(client, jump *ssh.Client) {
	if client != nil {
		_ = client.Close()
	}
	if jump != nil && jump != client {
		_ = jump.Close()
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
