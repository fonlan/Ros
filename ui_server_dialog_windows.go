//go:build windows

package main

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/lxn/walk"
	. "github.com/lxn/walk/declarative"
)

func showServerDialog(owner walk.Form, initial *ServerConfig) (*ServerConfig, bool, error) {
	working := cloneServerConfig(initial)

	var dlg *walk.Dialog
	var nameLE, userLE, passLE, domainLE *walk.LineEdit
	var adaptiveCB, diskCB, soundCB, clipboardCB *walk.CheckBox
	var widthLE, heightLE *walk.LineEdit
	var tunnelList *walk.ListBox

	tunnelItems := func() []string {
		items := make([]string, 0, len(working.Tunnels))
		for idx, tunnel := range working.Tunnels {
			jumpInfo := "direct"
			if tunnel.JumpHost.SSHHost != "" {
				jumpInfo = fmt.Sprintf(
					"jump=%s@%s:%d",
					tunnel.JumpHost.SSHUser,
					tunnel.JumpHost.SSHHost,
					tunnel.JumpHost.SSHPort,
				)
			}
			items = append(items, fmt.Sprintf(
				"%d. %s | %s@%s:%d -> %s:%d | %s | proxy=%s",
				idx+1,
				displayTunnelName(tunnel),
				tunnel.SSHUser,
				tunnel.SSHHost,
				tunnel.SSHPort,
				tunnel.RemoteHost,
				tunnel.RemotePort,
				jumpInfo,
				tunnel.Proxy.Type,
			))
		}
		return items
	}

	refreshTunnelModel := func() {
		if tunnelList == nil {
			return
		}
		_ = tunnelList.SetModel(tunnelItems())
	}

	reindexTunnelPriorities := func() {
		for i, tunnel := range working.Tunnels {
			if tunnel == nil {
				continue
			}
			tunnel.Priority = i + 1
		}
	}

	swapTunnel := func(i, j int) {
		if i < 0 || j < 0 || i >= len(working.Tunnels) || j >= len(working.Tunnels) {
			return
		}
		working.Tunnels[i], working.Tunnels[j] = working.Tunnels[j], working.Tunnels[i]
		reindexTunnelPriorities()
		refreshTunnelModel()
		tunnelList.SetCurrentIndex(j)
	}

	title := "新增服务器"
	if initial != nil {
		title = "编辑服务器"
	}

	err := Dialog{
		AssignTo: &dlg,
		Title:    title,
		MinSize:  Size{Width: 560, Height: 430},
		Layout:   VBox{},
		Children: []Widget{
			TabWidget{
				Pages: []TabPage{
					{
						Title:  "基础信息",
						Layout: Grid{Columns: 2},
						Children: []Widget{
							Label{Text: "服务器名称"},
							LineEdit{AssignTo: &nameLE, Text: working.Name},
							Label{Text: "RDP 用户名"},
							LineEdit{AssignTo: &userLE, Text: working.RDP.Username},
							Label{Text: "RDP 密码"},
							LineEdit{AssignTo: &passLE, Text: working.RDP.Password, PasswordMode: true},
							Label{Text: "域（可选）"},
							LineEdit{AssignTo: &domainLE, Text: working.RDP.Domain},
						},
					},
					{
						Title:  "SSH 隧道",
						Layout: VBox{},
						Children: []Widget{
							Label{
								Text: "连接时按列表顺序（优先级）逐个尝试，直到某个隧道建立成功。",
							},
							ListBox{
								AssignTo:      &tunnelList,
								Model:         tunnelItems(),
								MinSize:       Size{Width: 420, Height: 150},
								StretchFactor: 1,
							},
							Composite{
								Layout: HBox{},
								Children: []Widget{
									PushButton{
										Text: "新增隧道",
										OnClicked: func() {
											tunnel, ok, err := showTunnelDialog(dlg, nil, len(working.Tunnels)+1)
											if err != nil {
												walk.MsgBox(dlg, "错误", err.Error(), walk.MsgBoxIconError)
												return
											}
											if !ok || tunnel == nil {
												return
											}
											working.Tunnels = append(working.Tunnels, tunnel)
											reindexTunnelPriorities()
											refreshTunnelModel()
											tunnelList.SetCurrentIndex(len(working.Tunnels) - 1)
										},
									},
									PushButton{
										Text: "编辑隧道",
										OnClicked: func() {
											idx := tunnelList.CurrentIndex()
											if idx < 0 || idx >= len(working.Tunnels) {
												walk.MsgBox(dlg, "提示", "请先选中隧道", walk.MsgBoxIconInformation)
												return
											}
											tunnel, ok, err := showTunnelDialog(dlg, working.Tunnels[idx], idx+1)
											if err != nil {
												walk.MsgBox(dlg, "错误", err.Error(), walk.MsgBoxIconError)
												return
											}
											if !ok || tunnel == nil {
												return
											}
											working.Tunnels[idx] = tunnel
											reindexTunnelPriorities()
											refreshTunnelModel()
											tunnelList.SetCurrentIndex(idx)
										},
									},
									PushButton{
										Text: "删除隧道",
										OnClicked: func() {
											idx := tunnelList.CurrentIndex()
											if idx < 0 || idx >= len(working.Tunnels) {
												walk.MsgBox(dlg, "提示", "请先选中隧道", walk.MsgBoxIconInformation)
												return
											}
											working.Tunnels = append(working.Tunnels[:idx], working.Tunnels[idx+1:]...)
											reindexTunnelPriorities()
											refreshTunnelModel()
											if idx >= len(working.Tunnels) {
												idx = len(working.Tunnels) - 1
											}
											tunnelList.SetCurrentIndex(idx)
										},
									},
									HSpacer{},
									PushButton{
										Text: "上移",
										OnClicked: func() {
											idx := tunnelList.CurrentIndex()
											swapTunnel(idx, idx-1)
										},
									},
									PushButton{
										Text: "下移",
										OnClicked: func() {
											idx := tunnelList.CurrentIndex()
											swapTunnel(idx, idx+1)
										},
									},
								},
							},
						},
					},
					{
						Title:  "RDP 选项",
						Layout: Grid{Columns: 2},
						Children: []Widget{
							CheckBox{
								AssignTo:   &adaptiveCB,
								Text:       "分辨率自适应（使用当前主屏幕）",
								Checked:    working.RDP.AdaptiveResolution,
								ColumnSpan: 2,
								OnCheckedChanged: func() {
									enabled := !adaptiveCB.Checked()
									widthLE.SetEnabled(enabled)
									heightLE.SetEnabled(enabled)
								},
							},
							Label{Text: "固定宽度"},
							LineEdit{AssignTo: &widthLE, Text: strconv.Itoa(working.RDP.DesktopWidth)},
							Label{Text: "固定高度"},
							LineEdit{AssignTo: &heightLE, Text: strconv.Itoa(working.RDP.DesktopHeight)},
							CheckBox{
								AssignTo:   &diskCB,
								Text:       "磁盘重定向",
								Checked:    working.RDP.RedirectDisks,
								ColumnSpan: 2,
							},
							CheckBox{
								AssignTo:   &soundCB,
								Text:       "声音重定向",
								Checked:    working.RDP.RedirectSound,
								ColumnSpan: 2,
							},
							CheckBox{
								AssignTo:   &clipboardCB,
								Text:       "剪切板同步",
								Checked:    working.RDP.RedirectClipboard,
								ColumnSpan: 2,
							},
						},
					},
				},
			},
			Composite{
				Layout: HBox{},
				Children: []Widget{
					HSpacer{},
					PushButton{
						Text: "保存",
						OnClicked: func() {
							name := strings.TrimSpace(nameLE.Text())
							if name == "" {
								walk.MsgBox(dlg, "校验失败", "服务器名称不能为空", walk.MsgBoxIconWarning)
								return
							}
							if len(working.Tunnels) == 0 {
								walk.MsgBox(dlg, "校验失败", "至少需要 1 条 SSH 隧道", walk.MsgBoxIconWarning)
								return
							}

							working.Name = name
							working.RDP.Username = strings.TrimSpace(userLE.Text())
							working.RDP.Password = passLE.Text()
							working.RDP.Domain = strings.TrimSpace(domainLE.Text())
							working.RDP.AdaptiveResolution = adaptiveCB.Checked()
							working.RDP.RedirectDisks = diskCB.Checked()
							working.RDP.RedirectSound = soundCB.Checked()
							working.RDP.RedirectClipboard = clipboardCB.Checked()

							if !working.RDP.AdaptiveResolution {
								width, err := strconv.Atoi(strings.TrimSpace(widthLE.Text()))
								if err != nil || width <= 0 {
									walk.MsgBox(dlg, "校验失败", "固定宽度必须是正整数", walk.MsgBoxIconWarning)
									return
								}
								height, err := strconv.Atoi(strings.TrimSpace(heightLE.Text()))
								if err != nil || height <= 0 {
									walk.MsgBox(dlg, "校验失败", "固定高度必须是正整数", walk.MsgBoxIconWarning)
									return
								}
								working.RDP.DesktopWidth = width
								working.RDP.DesktopHeight = height
							}

							normalizeServerConfig(working)
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

	widthLE.SetEnabled(!working.RDP.AdaptiveResolution)
	heightLE.SetEnabled(!working.RDP.AdaptiveResolution)

	if dlg.Run() != int(walk.DlgCmdOK) {
		return nil, false, nil
	}
	return working, true, nil
}
