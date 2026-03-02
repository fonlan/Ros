//go:build windows

package main

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/lxn/walk"
	. "github.com/lxn/walk/declarative"
)

var (
	authTypeItems  = []string{"password", "key"}
	proxyTypeItems = []string{"none", "http", "socks5"}
)

func showTunnelDialog(owner walk.Form, initial *TunnelConfig, defaultPriority int) (*TunnelConfig, bool, error) {
	working := cloneTunnelConfig(initial)
	if working.Priority <= 0 {
		working.Priority = defaultPriority
	}

	var dlg *walk.Dialog
	var (
		nameLE, hostLE, portLE, userLE                             *walk.LineEdit
		authTypeCB                                                 *walk.ComboBox
		passwordLE, privateKeyPathLE, privateKeyPassLE             *walk.LineEdit
		jumpHostLE, jumpPortLE, jumpUserLE                         *walk.LineEdit
		jumpAuthTypeCB                                             *walk.ComboBox
		jumpPasswordLE, jumpPrivateKeyPathLE, jumpPrivateKeyPassLE *walk.LineEdit
		remoteHostLE, remotePortLE, timeoutLE                      *walk.LineEdit
		proxyTypeCB                                                *walk.ComboBox
		proxyAddrLE, proxyUserLE, proxyPassLE                      *walk.LineEdit
	)

	authTypeIndex := indexOf(authTypeItems, strings.ToLower(working.AuthType))
	if authTypeIndex < 0 {
		authTypeIndex = 0
	}

	proxyTypeIndex := indexOf(proxyTypeItems, strings.ToLower(working.Proxy.Type))
	if proxyTypeIndex < 0 {
		proxyTypeIndex = 0
	}

	jumpAuthTypeIndex := indexOf(authTypeItems, strings.ToLower(working.JumpHost.AuthType))
	if jumpAuthTypeIndex < 0 {
		jumpAuthTypeIndex = 0
	}
	jumpPortText := ""
	if working.JumpHost.SSHPort > 0 {
		jumpPortText = strconv.Itoa(working.JumpHost.SSHPort)
	}

	title := "新增隧道"
	if initial != nil {
		title = "编辑隧道"
	}

	err := Dialog{
		AssignTo: &dlg,
		Title:    title,
		MinSize:  Size{Width: 620, Height: 700},
		Layout:   VBox{},
		Children: []Widget{
			GroupBox{
				Title:  "SSH 配置",
				Layout: Grid{Columns: 2},
				Children: []Widget{
					Label{Text: "隧道名称"},
					LineEdit{AssignTo: &nameLE, Text: working.Name},
					Label{Text: "SSH Host"},
					LineEdit{AssignTo: &hostLE, Text: working.SSHHost},
					Label{Text: "SSH Port"},
					LineEdit{AssignTo: &portLE, Text: strconv.Itoa(working.SSHPort)},
					Label{Text: "SSH 用户名"},
					LineEdit{AssignTo: &userLE, Text: working.SSHUser},
					Label{Text: "认证方式"},
					ComboBox{
						AssignTo:     &authTypeCB,
						Model:        authTypeItems,
						CurrentIndex: authTypeIndex,
					},
					Label{Text: "SSH 密码（password）"},
					LineEdit{AssignTo: &passwordLE, Text: working.Password, PasswordMode: true},
					Label{Text: "私钥路径（key）"},
					LineEdit{AssignTo: &privateKeyPathLE, Text: working.PrivateKeyPath},
					Label{Text: "私钥口令（可选）"},
					LineEdit{AssignTo: &privateKeyPassLE, Text: working.PrivateKeyPassphrase, PasswordMode: true},
				},
			},
			GroupBox{
				Title:  "SSH 跳板机（可选）",
				Layout: Grid{Columns: 2},
				Children: []Widget{
					Label{Text: "跳板机 Host（留空则不启用）"},
					LineEdit{AssignTo: &jumpHostLE, Text: working.JumpHost.SSHHost},
					Label{Text: "跳板机 Port"},
					LineEdit{AssignTo: &jumpPortLE, Text: jumpPortText},
					Label{Text: "跳板机用户名"},
					LineEdit{AssignTo: &jumpUserLE, Text: working.JumpHost.SSHUser},
					Label{Text: "跳板机认证方式"},
					ComboBox{
						AssignTo:     &jumpAuthTypeCB,
						Model:        authTypeItems,
						CurrentIndex: jumpAuthTypeIndex,
					},
					Label{Text: "跳板机密码（password）"},
					LineEdit{AssignTo: &jumpPasswordLE, Text: working.JumpHost.Password, PasswordMode: true},
					Label{Text: "跳板机私钥路径（key）"},
					LineEdit{AssignTo: &jumpPrivateKeyPathLE, Text: working.JumpHost.PrivateKeyPath},
					Label{Text: "跳板机私钥口令（可选）"},
					LineEdit{AssignTo: &jumpPrivateKeyPassLE, Text: working.JumpHost.PrivateKeyPassphrase, PasswordMode: true},
				},
			},
			GroupBox{
				Title:  "端口映射",
				Layout: Grid{Columns: 2},
				Children: []Widget{
					Label{Text: "远端 RDP Host"},
					LineEdit{AssignTo: &remoteHostLE, Text: working.RemoteHost},
					Label{Text: "远端 RDP Port"},
					LineEdit{AssignTo: &remotePortLE, Text: strconv.Itoa(working.RemotePort)},
					Label{Text: "连接超时（秒）"},
					LineEdit{AssignTo: &timeoutLE, Text: strconv.Itoa(working.ConnectTimeoutSec)},
				},
			},
			GroupBox{
				Title:  "代理配置",
				Layout: Grid{Columns: 2},
				Children: []Widget{
					Label{Text: "说明"},
					Label{Text: "启用跳板机时，代理仅用于连接跳板机"},
					Label{Text: "代理类型"},
					ComboBox{
						AssignTo:     &proxyTypeCB,
						Model:        proxyTypeItems,
						CurrentIndex: proxyTypeIndex,
					},
					Label{Text: "代理地址（host:port）"},
					LineEdit{AssignTo: &proxyAddrLE, Text: working.Proxy.Address},
					Label{Text: "代理用户名（可选）"},
					LineEdit{AssignTo: &proxyUserLE, Text: working.Proxy.Username},
					Label{Text: "代理密码（可选）"},
					LineEdit{AssignTo: &proxyPassLE, Text: working.Proxy.Password, PasswordMode: true},
				},
			},
			Composite{
				Layout: HBox{},
				Children: []Widget{
					HSpacer{},
					PushButton{
						Text: "保存",
						OnClicked: func() {
							tunnel, err := collectTunnelFromDialog(
								working,
								nameLE, hostLE, portLE, userLE,
								authTypeCB, passwordLE, privateKeyPathLE, privateKeyPassLE,
								jumpHostLE, jumpPortLE, jumpUserLE,
								jumpAuthTypeCB, jumpPasswordLE, jumpPrivateKeyPathLE, jumpPrivateKeyPassLE,
								remoteHostLE, remotePortLE, timeoutLE,
								proxyTypeCB, proxyAddrLE, proxyUserLE, proxyPassLE,
							)
							if err != nil {
								walk.MsgBox(dlg, "校验失败", err.Error(), walk.MsgBoxIconWarning)
								return
							}
							*working = *tunnel
							dlg.Accept()
						},
					},
					PushButton{
						Text: "取消",
						OnClicked: func() {
							dlg.Cancel()
						},
					},
				},
			},
		},
	}.Create(owner)
	if err != nil {
		return nil, false, err
	}

	hideDialogTitleIcon(dlg)

	if dlg.Run() != int(walk.DlgCmdOK) {
		return nil, false, nil
	}

	return working, true, nil
}

func collectTunnelFromDialog(
	base *TunnelConfig,
	nameLE, hostLE, portLE, userLE *walk.LineEdit,
	authTypeCB *walk.ComboBox,
	passwordLE, privateKeyPathLE, privateKeyPassLE *walk.LineEdit,
	jumpHostLE, jumpPortLE, jumpUserLE *walk.LineEdit,
	jumpAuthTypeCB *walk.ComboBox,
	jumpPasswordLE, jumpPrivateKeyPathLE, jumpPrivateKeyPassLE *walk.LineEdit,
	remoteHostLE, remotePortLE, timeoutLE *walk.LineEdit,
	proxyTypeCB *walk.ComboBox,
	proxyAddrLE, proxyUserLE, proxyPassLE *walk.LineEdit,
) (*TunnelConfig, error) {
	next := cloneTunnelConfig(base)

	next.Name = strings.TrimSpace(nameLE.Text())
	if next.Name == "" {
		return nil, fmt.Errorf("隧道名称不能为空")
	}

	next.SSHHost = strings.TrimSpace(hostLE.Text())
	if next.SSHHost == "" {
		return nil, fmt.Errorf("SSH Host 不能为空")
	}

	sshPort, err := strconv.Atoi(strings.TrimSpace(portLE.Text()))
	if err != nil || sshPort <= 0 {
		return nil, fmt.Errorf("SSH Port 必须是正整数")
	}
	next.SSHPort = sshPort

	next.SSHUser = strings.TrimSpace(userLE.Text())
	if next.SSHUser == "" {
		return nil, fmt.Errorf("SSH 用户名不能为空")
	}

	authIdx := authTypeCB.CurrentIndex()
	if authIdx < 0 || authIdx >= len(authTypeItems) {
		authIdx = 0
	}
	next.AuthType = authTypeItems[authIdx]

	next.Password = passwordLE.Text()
	next.PrivateKeyPath = strings.TrimSpace(privateKeyPathLE.Text())
	next.PrivateKeyPassphrase = privateKeyPassLE.Text()

	if next.AuthType == "password" && next.Password == "" {
		return nil, fmt.Errorf("认证方式为 password 时，SSH 密码不能为空")
	}
	if next.AuthType == "key" && next.PrivateKeyPath == "" {
		return nil, fmt.Errorf("认证方式为 key 时，私钥路径不能为空")
	}

	next.JumpHost = JumpConfig{}
	next.JumpHost.SSHHost = strings.TrimSpace(jumpHostLE.Text())
	if next.JumpHost.SSHHost != "" {
		jumpPort, err := strconv.Atoi(strings.TrimSpace(jumpPortLE.Text()))
		if err != nil || jumpPort <= 0 {
			return nil, fmt.Errorf("跳板机 Port 必须是正整数")
		}
		next.JumpHost.SSHPort = jumpPort

		next.JumpHost.SSHUser = strings.TrimSpace(jumpUserLE.Text())
		if next.JumpHost.SSHUser == "" {
			return nil, fmt.Errorf("跳板机用户名不能为空")
		}

		jumpAuthIdx := jumpAuthTypeCB.CurrentIndex()
		if jumpAuthIdx < 0 || jumpAuthIdx >= len(authTypeItems) {
			jumpAuthIdx = 0
		}
		next.JumpHost.AuthType = authTypeItems[jumpAuthIdx]
		next.JumpHost.Password = jumpPasswordLE.Text()
		next.JumpHost.PrivateKeyPath = strings.TrimSpace(jumpPrivateKeyPathLE.Text())
		next.JumpHost.PrivateKeyPassphrase = jumpPrivateKeyPassLE.Text()

		if next.JumpHost.AuthType == "password" && next.JumpHost.Password == "" {
			return nil, fmt.Errorf("跳板机认证方式为 password 时，密码不能为空")
		}
		if next.JumpHost.AuthType == "key" && next.JumpHost.PrivateKeyPath == "" {
			return nil, fmt.Errorf("跳板机认证方式为 key 时，私钥路径不能为空")
		}
	}

	next.RemoteHost = strings.TrimSpace(remoteHostLE.Text())
	if next.RemoteHost == "" {
		return nil, fmt.Errorf("远端 RDP Host 不能为空")
	}

	remotePort, err := strconv.Atoi(strings.TrimSpace(remotePortLE.Text()))
	if err != nil || remotePort <= 0 {
		return nil, fmt.Errorf("远端 RDP Port 必须是正整数")
	}
	next.RemotePort = remotePort

	timeoutSec, err := strconv.Atoi(strings.TrimSpace(timeoutLE.Text()))
	if err != nil || timeoutSec <= 0 {
		return nil, fmt.Errorf("连接超时必须是正整数（秒）")
	}
	next.ConnectTimeoutSec = timeoutSec

	proxyIdx := proxyTypeCB.CurrentIndex()
	if proxyIdx < 0 || proxyIdx >= len(proxyTypeItems) {
		proxyIdx = 0
	}
	next.Proxy.Type = proxyTypeItems[proxyIdx]
	next.Proxy.Address = strings.TrimSpace(proxyAddrLE.Text())
	next.Proxy.Username = strings.TrimSpace(proxyUserLE.Text())
	next.Proxy.Password = proxyPassLE.Text()

	if next.Proxy.Type != "none" && next.Proxy.Address == "" {
		return nil, fmt.Errorf("启用代理时，代理地址不能为空")
	}

	normalizeTunnelConfig(next)
	return next, nil
}

func indexOf(items []string, target string) int {
	for i, item := range items {
		if item == target {
			return i
		}
	}
	return -1
}
