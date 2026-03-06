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
	var compressionCB, videoPlaybackCB, smartSizingCB, framebufferButtonsCB *walk.CheckBox
	var printersCB *walk.CheckBox
	var disableWallpaperCB, disableFullDragCB, disableMenuAnimsCB, disableThemesCB *walk.CheckBox
	var credSSPCB, useMultimonCB, remoteAppCB *walk.CheckBox
	var widthLE, heightLE *walk.LineEdit
	var driveStoreDirectLE, cameraStoreDirectLE, deviceStoreDirectLE *walk.LineEdit
	var connectionTypeLE, authenticationLevelLE, selectedMonitorsLE *walk.LineEdit
	var tunnelList *walk.ListBox

	tunnelItems := func() []string {
		items := make([]string, 0, len(working.Tunnels))
		for idx, tunnel := range working.Tunnels {
			enabledText := "启用"
			if !isTunnelEnabled(tunnel) {
				enabledText = "禁用"
			}
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
				"%d. [%s] %s | %s@%s:%d -> %s:%d | %s | proxy=%s",
				idx+1,
				enabledText,
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
		MinSize:  Size{Width: 760, Height: 650},
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
								Text: "连接时按列表顺序（优先级）逐个尝试已启用隧道，直到某个隧道建立成功。",
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
						Title: "RDP 选项",
						Layout: VBox{
							MarginsZero: true,
							SpacingZero: true,
							Alignment:   AlignHNearVNear,
						},
						Children: []Widget{
							ScrollView{
								StretchFactor:   1,
								HorizontalFixed: false,
								VerticalFixed:   false,
								Layout: VBox{
									MarginsZero: true,
									Alignment:   AlignHNearVNear,
								},
								Children: []Widget{
									GroupBox{
										Title: "基础选项",
										Layout: Grid{
											Columns:   2,
											Alignment: AlignHNearVNear,
										},
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
												AssignTo:   &soundCB,
												Text:       "声音重定向",
												Checked:    working.RDP.RedirectSound,
												ColumnSpan: 2,
											},
											CheckBox{
												AssignTo:   &clipboardCB,
												Text:       "剪贴板同步",
												Checked:    working.RDP.RedirectClipboard,
												ColumnSpan: 2,
											},
										},
									},
									GroupBox{
										Title: "显示与图形",
										Layout: VBox{
											Alignment: AlignHNearVNear,
										},
										Children: []Widget{
											CheckBox{
												AssignTo: &compressionCB,
												Text:     "启用图像压缩",
												Checked:  working.RDP.Compression,
											},
											CheckBox{
												AssignTo: &videoPlaybackCB,
												Text:     "启用视频播放优化",
												Checked:  working.RDP.VideoPlaybackMode,
											},
											CheckBox{
												AssignTo: &smartSizingCB,
												Text:     "启用窗口缩放（随窗口大小自动缩放）",
												Checked:  working.RDP.SmartSizing,
											},
											CheckBox{
												AssignTo: &framebufferButtonsCB,
												Text:     "启用图形交互增强",
												Checked:  working.RDP.FramebufferButtons,
											},
											HSpacer{},
										},
									},
									GroupBox{
										Title: "资源重定向",
										Layout: Grid{
											Columns:   2,
											Alignment: AlignHNearVNear,
										},
										Children: []Widget{
											CheckBox{
												AssignTo:   &diskCB,
												Text:       "启用磁盘重定向",
												Checked:    working.RDP.RedirectDisks,
												ColumnSpan: 2,
												OnCheckedChanged: func() {
													if driveStoreDirectLE != nil {
														driveStoreDirectLE.SetEnabled(diskCB.Checked())
													}
												},
											},
											Label{Text: "重定向磁盘列表（示例：* 或 C:\\;D:\\）"},
											LineEdit{
												AssignTo: &driveStoreDirectLE,
												Text:     working.RDP.DriveStoreDirect,
											},
											Label{Text: "摄像头重定向（留空禁用，* 表示全部）"},
											LineEdit{
												AssignTo: &cameraStoreDirectLE,
												Text:     working.RDP.CameraStoreDirect,
											},
											Label{Text: "即插即用设备重定向（留空禁用，* 表示全部）"},
											LineEdit{
												AssignTo: &deviceStoreDirectLE,
												Text:     working.RDP.DeviceStoreDirect,
											},
											CheckBox{
												AssignTo:   &printersCB,
												Text:       "本地打印机重定向",
												Checked:    working.RDP.RedirectPrinters,
												ColumnSpan: 2,
											},
										},
									},
									GroupBox{
										Title: "性能与连接优化",
										Layout: Grid{
											Columns:   2,
											Alignment: AlignHNearVNear,
										},
										Children: []Widget{
											Label{Text: "连接类型（1-7，推荐 6=局域网，7=自动）"},
											LineEdit{
												AssignTo: &connectionTypeLE,
												Text:     strconv.Itoa(working.RDP.ConnectionType),
											},
											CheckBox{
												AssignTo:   &disableWallpaperCB,
												Text:       "禁用桌面壁纸",
												Checked:    working.RDP.DisableWallpaper,
												ColumnSpan: 2,
											},
											CheckBox{
												AssignTo:   &disableFullDragCB,
												Text:       "禁用拖动窗口时显示内容",
												Checked:    working.RDP.DisableFullWindowDrag,
												ColumnSpan: 2,
											},
											CheckBox{
												AssignTo:   &disableMenuAnimsCB,
												Text:       "禁用菜单动画",
												Checked:    working.RDP.DisableMenuAnims,
												ColumnSpan: 2,
											},
											CheckBox{
												AssignTo:   &disableThemesCB,
												Text:       "禁用主题样式",
												Checked:    working.RDP.DisableThemes,
												ColumnSpan: 2,
											},
										},
									},
									GroupBox{
										Title: "安全与身份验证",
										Layout: Grid{
											Columns:   2,
											Alignment: AlignHNearVNear,
										},
										Children: []Widget{
											Label{Text: "身份验证级别（0=不验证，1=失败警告，2=失败拒绝）"},
											LineEdit{
												AssignTo: &authenticationLevelLE,
												Text:     strconv.Itoa(working.RDP.AuthenticationLevel),
											},
											CheckBox{
												AssignTo:   &credSSPCB,
												Text:       "启用网络级身份验证",
												Checked:    working.RDP.EnableCredSSPSupport,
												ColumnSpan: 2,
											},
										},
									},
									GroupBox{
										Title: "多显示器与远程应用",
										Layout: Grid{
											Columns:   2,
											Alignment: AlignHNearVNear,
										},
										Children: []Widget{
											CheckBox{
												AssignTo:   &useMultimonCB,
												Text:       "启用多显示器",
												Checked:    working.RDP.UseMultiMon,
												ColumnSpan: 2,
												OnCheckedChanged: func() {
													if selectedMonitorsLE != nil {
														selectedMonitorsLE.SetEnabled(useMultimonCB.Checked())
													}
												},
											},
											Label{Text: "指定显示器编号（示例：0,1）"},
											LineEdit{
												AssignTo: &selectedMonitorsLE,
												Text:     working.RDP.SelectedMonitors,
											},
											CheckBox{
												AssignTo:   &remoteAppCB,
												Text:       "启用远程应用模式",
												Checked:    working.RDP.RemoteApplicationMode,
												ColumnSpan: 2,
											},
										},
									},
								},
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
							working.RDP.Compression = compressionCB.Checked()
							working.RDP.VideoPlaybackMode = videoPlaybackCB.Checked()
							working.RDP.SmartSizing = smartSizingCB.Checked()
							working.RDP.FramebufferButtons = framebufferButtonsCB.Checked()
							if working.RDP.RedirectDisks {
								working.RDP.DriveStoreDirect = strings.TrimSpace(driveStoreDirectLE.Text())
							} else {
								working.RDP.DriveStoreDirect = ""
							}
							working.RDP.CameraStoreDirect = strings.TrimSpace(cameraStoreDirectLE.Text())
							working.RDP.DeviceStoreDirect = strings.TrimSpace(deviceStoreDirectLE.Text())
							working.RDP.RedirectPrinters = printersCB.Checked()
							working.RDP.DisableWallpaper = disableWallpaperCB.Checked()
							working.RDP.DisableFullWindowDrag = disableFullDragCB.Checked()
							working.RDP.DisableMenuAnims = disableMenuAnimsCB.Checked()
							working.RDP.DisableThemes = disableThemesCB.Checked()
							working.RDP.EnableCredSSPSupport = credSSPCB.Checked()
							working.RDP.UseMultiMon = useMultimonCB.Checked()
							working.RDP.SelectedMonitors = strings.TrimSpace(selectedMonitorsLE.Text())
							working.RDP.RemoteApplicationMode = remoteAppCB.Checked()
							working.RDP.AdvancedOptionsConfigured = true

							connType, err := strconv.Atoi(strings.TrimSpace(connectionTypeLE.Text()))
							if err != nil || connType < 1 || connType > 7 {
								walk.MsgBox(dlg, "校验失败", "连接类型必须是 1-7 之间的整数", walk.MsgBoxIconWarning)
								return
							}
							working.RDP.ConnectionType = connType

							authLevel, err := strconv.Atoi(strings.TrimSpace(authenticationLevelLE.Text()))
							if err != nil || authLevel < 0 || authLevel > 2 {
								walk.MsgBox(dlg, "校验失败", "身份验证级别必须是 0-2 之间的整数", walk.MsgBoxIconWarning)
								return
							}
							working.RDP.AuthenticationLevel = authLevel

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
	driveStoreDirectLE.SetEnabled(working.RDP.RedirectDisks)
	selectedMonitorsLE.SetEnabled(working.RDP.UseMultiMon)

	if dlg.Run() != int(walk.DlgCmdOK) {
		return nil, false, nil
	}
	return working, true, nil
}
