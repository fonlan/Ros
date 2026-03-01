//go:build windows

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/lxn/walk"
	. "github.com/lxn/walk/declarative"
)

type RosApp struct {
	configPath string
	cfg        *AppConfig

	mu sync.Mutex

	mw         *walk.MainWindow
	serverList *walk.ListBox
	statusLbl  *walk.Label
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

	a.refreshServerList()
	a.setStatus("就绪")

	a.mw.Run()
	return nil
}

func (a *RosApp) createMainWindow() error {
	if err := (MainWindow{
		AssignTo: &a.mw,
		Title:    "Ros - RDP SSH 辅助程序",
		Size:     Size{Width: 360, Height: 300},
		MinSize:  Size{Width: 360, Height: 300},
		Layout:   VBox{MarginsZero: false},
		Children: []Widget{
			Label{
				Text: "服务器列表（单击即连接）",
			},
			ListBox{
				AssignTo: &a.serverList,
				MinSize:  Size{Width: 320, Height: 180},
				OnCurrentIndexChanged: func() {
					a.onListSelectionChanged()
				},
			},
			Composite{
				Layout: HBox{},
				Children: []Widget{
					PushButton{
						Text: "新增服务器",
						OnClicked: func() {
							a.onAddClicked()
						},
					},
					PushButton{
						Text: "编辑服务器",
						OnClicked: func() {
							a.onEditClicked()
						},
					},
					PushButton{
						Text: "删除服务器",
						OnClicked: func() {
							a.onDeleteClicked()
						},
					},
					HSpacer{},
					PushButton{
						Text: "连接",
						OnClicked: func() {
							a.onConnectClicked()
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
	return nil
}

func (a *RosApp) applyMainWindowIcon() {
	if a.mw == nil {
		return
	}

	iconPath := "app.ico"
	if exePath, err := os.Executable(); err == nil {
		candidate := filepath.Join(filepath.Dir(exePath), "app.ico")
		if _, statErr := os.Stat(candidate); statErr == nil {
			iconPath = candidate
		}
	}

	icon, err := walk.NewIconFromFile(iconPath)
	if err != nil {
		return
	}
	_ = a.mw.SetIcon(icon)
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
	a.serverList.SetCurrentIndex(idx)
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

func (a *RosApp) onConnectClicked() {
	a.connectSelected(false)
}

func (a *RosApp) onListSelectionChanged() {
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

	waitErr := session.Wait()
	session.Cleanup()
	activeTunnel.Close()

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

func (a *RosApp) setStatus(text string) {
	if a.statusLbl != nil {
		a.statusLbl.SetText(text)
	}
}

func (a *RosApp) syncSetStatus(text string) {
	if a.mw == nil {
		return
	}
	a.mw.Synchronize(func() {
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
	if err == nil || a.mw == nil {
		return
	}
	a.mw.Synchronize(func() {
		a.showError(err)
	})
}

func (a *RosApp) showInfo(message string) {
	walk.MsgBox(a.mw, "提示", message, walk.MsgBoxIconInformation)
}
