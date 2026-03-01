//go:build windows

package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/lxn/win"
)

type RDPSession struct {
	cmd         *exec.Cmd
	rdpFilePath string
	credTargets []string
}

func StartRDPSession(server *ServerConfig, localPort int) (*RDPSession, error) {
	if strings.TrimSpace(server.RDP.Username) == "" {
		return nil, errors.New("RDP 用户名不能为空")
	}
	if server.RDP.Password == "" {
		return nil, errors.New("RDP 密码不能为空")
	}

	rdpPath, err := writeRDPTempFile(server, localPort)
	if err != nil {
		return nil, err
	}

	targets, err := createTemporaryCredentials(server.RDP, localPort)
	if err != nil {
		_ = os.Remove(rdpPath)
		return nil, err
	}

	cmd := exec.Command("mstsc", rdpPath)
	if err := cmd.Start(); err != nil {
		deleteCmdkeyTargets(targets)
		_ = os.Remove(rdpPath)
		return nil, fmt.Errorf("启动 mstsc 失败: %w", err)
	}

	return &RDPSession{
		cmd:         cmd,
		rdpFilePath: rdpPath,
		credTargets: targets,
	}, nil
}

func (s *RDPSession) Wait() error {
	if s == nil || s.cmd == nil {
		return nil
	}
	return s.cmd.Wait()
}

func (s *RDPSession) Cleanup() {
	if s == nil {
		return
	}
	deleteCmdkeyTargets(s.credTargets)
	if s.rdpFilePath != "" {
		_ = os.Remove(s.rdpFilePath)
	}
}

func writeRDPTempFile(server *ServerConfig, localPort int) (string, error) {
	width, height := resolveResolution(server.RDP)
	username := formatRDPUsername(server.RDP)

	lines := []string{
		"screen mode id:i:2",
		fmt.Sprintf("desktopwidth:i:%d", width),
		fmt.Sprintf("desktopheight:i:%d", height),
		"session bpp:i:32",
		"compression:i:1",
		"keyboardhook:i:2",
		fmt.Sprintf("full address:s:127.0.0.1:%d", localPort),
		fmt.Sprintf("server port:i:%d", localPort),
		fmt.Sprintf("username:s:%s", username),
		"prompt for credentials:i:0",
		"promptcredentialonce:i:1",
		"authentication level:i:2",
		"enablecredsspsupport:i:1",
		"negotiate security layer:i:1",
		fmt.Sprintf("redirectclipboard:i:%d", boolToInt(server.RDP.RedirectClipboard)),
		fmt.Sprintf("drivestoredirect:s:%s", driveRedirectValue(server.RDP.RedirectDisks)),
		fmt.Sprintf("audiomode:i:%d", audioMode(server.RDP.RedirectSound)),
		fmt.Sprintf("smart sizing:i:%d", boolToInt(server.RDP.AdaptiveResolution)),
		fmt.Sprintf("dynamic resolution:i:%d", boolToInt(server.RDP.AdaptiveResolution)),
	}

	content := strings.Join(lines, "\r\n") + "\r\n"
	fileName := fmt.Sprintf("ros-%s-%d.rdp", sanitizeFilePart(server.Name), localPort)
	path := filepath.Join(os.TempDir(), fileName)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		return "", fmt.Errorf("写入临时 RDP 文件失败: %w", err)
	}
	return path, nil
}

func createTemporaryCredentials(rdp RDPConfig, localPort int) ([]string, error) {
	username := formatRDPUsername(rdp)
	targets := uniqueStrings([]string{
		"TERMSRV/127.0.0.1",
		"TERMSRV/localhost",
		"TERMSRV/127.0.0.1:" + strconv.Itoa(localPort),
		"TERMSRV/localhost:" + strconv.Itoa(localPort),
	})

	for _, target := range targets {
		cmd := exec.Command("cmdkey", "/generic:"+target, "/user:"+username, "/pass:"+rdp.Password)
		if out, err := cmd.CombinedOutput(); err != nil {
			deleteCmdkeyTargets(targets)
			return nil, fmt.Errorf("创建临时凭据失败(%s): %v, %s", target, err, strings.TrimSpace(string(out)))
		}
	}
	return targets, nil
}

func deleteCmdkeyTargets(targets []string) {
	for _, target := range targets {
		cmd := exec.Command("cmdkey", "/delete:"+target)
		_, _ = cmd.CombinedOutput()
	}
}

func formatRDPUsername(rdp RDPConfig) string {
	if strings.TrimSpace(rdp.Domain) == "" {
		return strings.TrimSpace(rdp.Username)
	}

	username := strings.TrimSpace(rdp.Username)
	if strings.Contains(username, "\\") {
		return username
	}
	return strings.TrimSpace(rdp.Domain) + `\` + username
}

func resolveResolution(rdp RDPConfig) (int, int) {
	if rdp.AdaptiveResolution {
		width := int(win.GetSystemMetrics(win.SM_CXSCREEN))
		height := int(win.GetSystemMetrics(win.SM_CYSCREEN))
		if width > 0 && height > 0 {
			return width, height
		}
	}

	width := rdp.DesktopWidth
	height := rdp.DesktopHeight
	if width <= 0 {
		width = defaultDesktopWidth
	}
	if height <= 0 {
		height = defaultDesktopHigh
	}
	return width, height
}

func driveRedirectValue(allow bool) string {
	if allow {
		return "*"
	}
	return ""
}

func audioMode(redirect bool) int {
	if redirect {
		return 0
	}
	return 2
}

func boolToInt(v bool) int {
	if v {
		return 1
	}
	return 0
}

func sanitizeFilePart(v string) string {
	v = strings.TrimSpace(v)
	if v == "" {
		return "server"
	}

	var b strings.Builder
	for _, r := range v {
		switch {
		case r >= 'a' && r <= 'z':
			b.WriteRune(r)
		case r >= 'A' && r <= 'Z':
			b.WriteRune(r)
		case r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '-' || r == '_':
			b.WriteRune(r)
		default:
			b.WriteRune('_')
		}
	}

	return b.String()
}

func uniqueStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}
