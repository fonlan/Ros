//go:build windows

package main

import (
	"fmt"
	"sync"
	"unsafe"

	"github.com/lxn/walk"
	. "github.com/lxn/walk/declarative"
	"github.com/lxn/win"
)

type RosApp struct {
	configPath string
	cfg        *AppConfig

	mu sync.Mutex

	mw         *walk.MainWindow
	serverList *walk.ListBox
	statusLbl  *walk.Label
	notifyIcon *walk.NotifyIcon
	appIcon    *walk.Icon

	suppressConnectOnce    bool
	triggerConnectOnSelect bool
	allowWindowClose       bool
	trayHintShown          bool
}

func NewRosApp() (*RosApp, error) {
	configPath, cfg, err := loadConfig()
	if err != nil {
		return nil, err
	}

	return &RosApp{
		configPath: configPath,
		cfg:        cfg,
	}, nil
}

func (a *RosApp) Run() error {
	if err := a.createMainWindow(); err != nil {
		return err
	}
	defer a.disposeNotifyIcon()

	a.refreshServerList()
	a.setStatus("就绪")
	a.centerMainWindow()
	a.mw.Show()

	a.mw.Run()
	return nil
}

func (a *RosApp) createMainWindow() error {
	if err := (MainWindow{
		AssignTo: &a.mw,
		Title:    "Ros - RDP over SSH",
		Size:     Size{Width: 360, Height: 300},
		MinSize:  Size{Width: 360, Height: 300},
		Visible:  false,
		Layout:   VBox{MarginsZero: false},
		Children: []Widget{
			Composite{
				Layout: HBox{MarginsZero: true},
				Children: []Widget{
					Label{
						Text: "服务器列表（单击连接）",
					},
					HSpacer{},
					PushButton{
						Text:        "\uE710",
						Font:        Font{Family: "Segoe MDL2 Assets", PointSize: 10},
						MinSize:     Size{Width: 36, Height: 28},
						MaxSize:     Size{Width: 36, Height: 28},
						ToolTipText: "新增服务器",
						OnClicked: func() {
							a.onAddClicked()
						},
					},
				},
			},
			ListBox{
				AssignTo:      &a.serverList,
				MinSize:       Size{Width: 320, Height: 190},
				StretchFactor: 1,
				OnCurrentIndexChanged: func() {
					a.onListSelectionChanged()
				},
				OnMouseDown: func(x, y int, button walk.MouseButton) {
					a.onServerListMouseDown(x, y, button)
				},
				ContextMenuItems: []MenuItem{
					Action{
						Text: "编辑服务器",
						OnTriggered: func() {
							a.onEditClicked()
						},
					},
					Action{
						Text: "删除服务器",
						OnTriggered: func() {
							a.onDeleteClicked()
						},
					},
				},
			},
			Label{
				AssignTo: &a.statusLbl,
				Text:     "就绪",
			},
		},
	}).Create(); err != nil {
		return err
	}

	a.applyMainWindowIcon()
	if err := a.initNotifyIcon(); err != nil {
		return err
	}
	a.bindMainWindowEvents()
	return nil
}

func (a *RosApp) applyMainWindowIcon() {
	if a.mw == nil {
		return
	}

	if a.appIcon == nil {
		a.appIcon = a.loadAppIcon()
	}
	if a.appIcon == nil {
		return
	}
	_ = a.mw.SetIcon(a.appIcon)
}

func (a *RosApp) loadAppIcon() *walk.Icon {
	// rsrc with manifest typically assigns group icon ID 2; without manifest it may be 1.
	for _, id := range []int{2, 1} {
		if icon, err := walk.NewIconFromResourceId(id); err == nil {
			return icon
		}
	}

	for id := 3; id <= 16; id++ {
		if icon, err := walk.NewIconFromResourceId(id); err == nil {
			return icon
		}
	}

	return nil
}

func (a *RosApp) initNotifyIcon() error {
	if a.mw == nil {
		return nil
	}

	ni, err := walk.NewNotifyIcon(a.mw)
	if err != nil {
		return fmt.Errorf("初始化系统托盘失败: %w", err)
	}
	fail := func(prefix string, err error) error {
		_ = ni.Dispose()
		return fmt.Errorf("%s: %w", prefix, err)
	}

	if a.appIcon != nil {
		if err := ni.SetIcon(a.appIcon); err != nil {
			return fail("设置托盘图标失败", err)
		}
	}
	if err := ni.SetToolTip("Ros - RDP SSH 辅助程序"); err != nil {
		return fail("设置托盘提示失败", err)
	}

	showAction := walk.NewAction()
	if err := showAction.SetText("显示主界面"); err != nil {
		return fail("创建托盘菜单失败", err)
	}
	showAction.Triggered().Attach(func() {
		a.showMainWindowFromTray()
	})

	exitAction := walk.NewAction()
	if err := exitAction.SetText("退出"); err != nil {
		return fail("创建托盘菜单失败", err)
	}
	exitAction.Triggered().Attach(func() {
		a.exitFromTray()
	})

	if err := ni.ContextMenu().Actions().Add(showAction); err != nil {
		return fail("添加托盘菜单失败", err)
	}
	if err := ni.ContextMenu().Actions().Add(exitAction); err != nil {
		return fail("添加托盘菜单失败", err)
	}

	ni.MouseUp().Attach(func(x, y int, button walk.MouseButton) {
		if button == walk.LeftButton {
			a.showMainWindowFromTray()
		}
	})

	if err := ni.SetVisible(true); err != nil {
		return fail("显示托盘图标失败", err)
	}

	a.notifyIcon = ni
	return nil
}

func (a *RosApp) bindMainWindowEvents() {
	if a.mw == nil {
		return
	}

	a.mw.Closing().Attach(func(canceled *bool, _ walk.CloseReason) {
		if a.allowWindowClose {
			return
		}
		*canceled = true
		a.hideMainWindowToTray(true)
	})
}

func (a *RosApp) centerMainWindow() {
	if a.mw == nil || a.mw.IsDisposed() {
		return
	}

	bounds := a.mw.BoundsPixels()
	if bounds.Width <= 0 || bounds.Height <= 0 {
		return
	}

	workArea := win.RECT{
		Left:   0,
		Top:    0,
		Right:  int32(win.GetSystemMetrics(win.SM_CXSCREEN)),
		Bottom: int32(win.GetSystemMetrics(win.SM_CYSCREEN)),
	}

	monitor := win.MonitorFromWindow(a.mw.Handle(), win.MONITOR_DEFAULTTONEAREST)
	if monitor != 0 {
		mi := win.MONITORINFO{CbSize: uint32(unsafe.Sizeof(win.MONITORINFO{}))}
		if win.GetMonitorInfo(monitor, &mi) {
			workArea = mi.RcWork
		}
	}

	workWidth := int(workArea.Right - workArea.Left)
	workHeight := int(workArea.Bottom - workArea.Top)
	x := int(workArea.Left) + (workWidth-bounds.Width)/2
	y := int(workArea.Top) + (workHeight-bounds.Height)/2

	if x < int(workArea.Left) {
		x = int(workArea.Left)
	}
	if y < int(workArea.Top) {
		y = int(workArea.Top)
	}

	_ = a.mw.SetBoundsPixels(walk.Rectangle{
		X:      x,
		Y:      y,
		Width:  bounds.Width,
		Height: bounds.Height,
	})
}

func (a *RosApp) hideMainWindowToTray(showTip bool) {
	if a.mw == nil || a.mw.IsDisposed() {
		return
	}

	a.mw.Hide()
	if showTip && !a.trayHintShown && a.notifyIcon != nil {
		_ = a.notifyIcon.ShowInfo("Ros", "程序已最小化到托盘，点击托盘图标可恢复主界面。")
		a.trayHintShown = true
	}
}

func (a *RosApp) showMainWindowFromTray() {
	if a.mw == nil || a.mw.IsDisposed() {
		return
	}

	a.mw.Show()
	win.ShowWindow(a.mw.Handle(), win.SW_RESTORE)
	win.SetForegroundWindow(a.mw.Handle())
}

func (a *RosApp) exitFromTray() {
	if a.mw == nil || a.mw.IsDisposed() {
		return
	}

	a.syncUI(func() {
		a.allowWindowClose = true
		a.disposeNotifyIcon()
		_ = a.mw.Close()
		if a.mw != nil && !a.mw.IsDisposed() {
			a.mw.Dispose()
		}
		walk.App().Exit(0)
	})
}

func (a *RosApp) disposeNotifyIcon() {
	if a.notifyIcon == nil {
		return
	}

	_ = a.notifyIcon.SetVisible(false)
	_ = a.notifyIcon.Dispose()
	a.notifyIcon = nil
}

func (a *RosApp) onAddClicked() {
	server, ok, err := showServerDialog(a.mw, nil)
	if err != nil {
		a.showError(err)
		return
	}
	if !ok || server == nil {
		return
	}

	a.mu.Lock()
	a.cfg.Servers = append(a.cfg.Servers, server)
	err = saveConfig(a.configPath, a.cfg)
	a.mu.Unlock()
	if err != nil {
		a.showError(err)
		return
	}

	a.refreshServerList()
	a.setStatus(fmt.Sprintf("已新增服务器: %s", server.Name))
}

func (a *RosApp) onEditClicked() {
	server, idx := a.selectedServer()
	if server == nil {
		a.showInfo("请先选中服务器")
		return
	}

	updated, ok, err := showServerDialog(a.mw, server)
	if err != nil {
		a.showError(err)
		return
	}
	if !ok || updated == nil {
		return
	}

	a.mu.Lock()
	a.cfg.Servers[idx] = updated
	err = saveConfig(a.configPath, a.cfg)
	a.mu.Unlock()
	if err != nil {
		a.showError(err)
		return
	}

	a.refreshServerList()
	a.setStatus(fmt.Sprintf("已更新服务器: %s", updated.Name))
}

func (a *RosApp) onDeleteClicked() {
	server, idx := a.selectedServer()
	if server == nil {
		a.showInfo("请先选中服务器")
		return
	}

	msg := fmt.Sprintf("确认删除服务器 \"%s\" 吗？", server.Name)
	result := walk.MsgBox(a.mw, "删除确认", msg, walk.MsgBoxIconQuestion|walk.MsgBoxYesNo)
	if result != walk.DlgCmdYes {
		return
	}

	a.mu.Lock()
	a.cfg.Servers = append(a.cfg.Servers[:idx], a.cfg.Servers[idx+1:]...)
	err := saveConfig(a.configPath, a.cfg)
	a.mu.Unlock()
	if err != nil {
		a.showError(err)
		return
	}

	a.refreshServerList()
	a.setStatus(fmt.Sprintf("已删除服务器: %s", server.Name))
}

func (a *RosApp) onListSelectionChanged() {
	if a.suppressConnectOnce {
		a.suppressConnectOnce = false
		a.triggerConnectOnSelect = false
		return
	}

	if !a.triggerConnectOnSelect {
		return
	}
	a.triggerConnectOnSelect = false
	a.connectSelected(true)
}

func (a *RosApp) connectSelected(silentIfNone bool) {
	server, _ := a.selectedServer()
	if server == nil {
		if !silentIfNone {
			a.showInfo("请先选中服务器")
		}
		return
	}

	serverCopy := cloneServerConfig(server)
	a.setStatus(fmt.Sprintf("开始连接: %s", serverCopy.Name))

	go a.connectServer(serverCopy)
}

func (a *RosApp) connectServer(server *ServerConfig) {
	activeTunnel, usedTunnel, err := StartTunnelWithFallback(server.Tunnels)
	if err != nil {
		a.syncShowError(fmt.Errorf("建立 SSH 隧道失败: %w", err))
		return
	}

	a.syncSetStatus(fmt.Sprintf(
		"隧道建立成功: %s -> 127.0.0.1:%d",
		displayTunnelName(usedTunnel),
		activeTunnel.LocalPort,
	))

	session, err := StartRDPSession(server, activeTunnel.LocalPort)
	if err != nil {
		activeTunnel.Close()
		a.syncShowError(fmt.Errorf("启动远程桌面失败: %w", err))
		return
	}

	a.syncSetStatus(fmt.Sprintf("已启动 mstsc，正在连接: %s", server.Name))
	a.syncHideMainWindowToTray(false)

	waitErr := session.Wait()
	session.Cleanup()
	activeTunnel.Close()
	a.syncShowMainWindowFromTray()

	if waitErr != nil {
		a.syncSetStatus(fmt.Sprintf("远程桌面会话结束（异常）: %v", waitErr))
		return
	}
	a.syncSetStatus(fmt.Sprintf("远程桌面会话结束: %s", server.Name))
}

func (a *RosApp) selectedServer() (*ServerConfig, int) {
	if a.serverList == nil {
		return nil, -1
	}
	idx := a.serverList.CurrentIndex()
	if idx < 0 || idx >= len(a.cfg.Servers) {
		return nil, -1
	}
	return a.cfg.Servers[idx], idx
}

func (a *RosApp) refreshServerList() {
	items := make([]string, 0, len(a.cfg.Servers))
	for _, server := range a.cfg.Servers {
		items = append(items, fmt.Sprintf("%s  (隧道 %d)", server.Name, len(server.Tunnels)))
	}
	_ = a.serverList.SetModel(items)
}

func (a *RosApp) onServerListMouseDown(x, y int, button walk.MouseButton) {
	if a.serverList == nil {
		return
	}

	idx := a.serverListIndexAt(x, y)

	if button&walk.RightButton != 0 {
		if idx >= 0 && idx < len(a.cfg.Servers) && idx != a.serverList.CurrentIndex() {
			a.suppressConnectOnce = true
			_ = a.serverList.SetCurrentIndex(idx)
		}
		return
	}

	if button&walk.LeftButton == 0 || idx < 0 || idx >= len(a.cfg.Servers) {
		return
	}

	if idx == a.serverList.CurrentIndex() {
		a.connectSelected(true)
		return
	}

	a.triggerConnectOnSelect = true
	_ = a.serverList.SetCurrentIndex(idx)
}

func (a *RosApp) serverListIndexAt(x, y int) int {
	if a.serverList == nil {
		return -1
	}

	lp := uintptr((uint32(y)&0xFFFF)<<16 | (uint32(x) & 0xFFFF))
	result := uint32(a.serverList.SendMessage(win.LB_ITEMFROMPOINT, 0, lp))
	if win.HIWORD(result) != 0 {
		return -1
	}
	return int(win.LOWORD(result))
}

func (a *RosApp) setStatus(text string) {
	if a.statusLbl != nil {
		a.statusLbl.SetText(text)
	}
}

func (a *RosApp) syncSetStatus(text string) {
	a.syncUI(func() {
		a.setStatus(text)
	})
}

func (a *RosApp) showError(err error) {
	if err == nil {
		return
	}
	walk.MsgBox(a.mw, "错误", err.Error(), walk.MsgBoxIconError)
}

func (a *RosApp) syncShowError(err error) {
	if err == nil {
		return
	}
	a.syncUI(func() {
		a.showError(err)
	})
}

func (a *RosApp) syncHideMainWindowToTray(showTip bool) {
	a.syncUI(func() {
		a.hideMainWindowToTray(showTip)
	})
}

func (a *RosApp) syncShowMainWindowFromTray() {
	a.syncUI(func() {
		a.showMainWindowFromTray()
	})
}

func (a *RosApp) syncUI(fn func()) {
	if fn == nil || a.mw == nil || a.mw.IsDisposed() {
		return
	}
	a.mw.Synchronize(func() {
		if a.mw == nil || a.mw.IsDisposed() {
			return
		}
		fn()
	})
}

func (a *RosApp) showInfo(message string) {
	walk.MsgBox(a.mw, "提示", message, walk.MsgBoxIconInformation)
}
