//go:build windows

package main

import (
	"github.com/lxn/walk"
	"github.com/lxn/win"
)

func hideDialogTitleIcon(dlg *walk.Dialog) {
	if dlg == nil {
		return
	}

	hWnd := dlg.Handle()
	if hWnd == 0 {
		return
	}

	// 使用工具窗口 + 对话框边框样式，避免标题栏回退显示类图标。
	exStyle := uint32(win.GetWindowLong(hWnd, win.GWL_EXSTYLE))
	exStyle |= win.WS_EX_DLGMODALFRAME | win.WS_EX_TOOLWINDOW
	_ = win.SetWindowLong(hWnd, win.GWL_EXSTYLE, int32(exStyle))

	_ = dlg.SetIcon(nil)
	win.SendMessage(hWnd, win.WM_SETICON, uintptr(0), 0)
	win.SendMessage(hWnd, win.WM_SETICON, uintptr(1), 0)
	win.SendMessage(hWnd, win.WM_SETICON, uintptr(2), 0)
	win.SetWindowPos(
		hWnd,
		0,
		0,
		0,
		0,
		0,
		win.SWP_NOMOVE|win.SWP_NOSIZE|win.SWP_NOZORDER|win.SWP_FRAMECHANGED,
	)
	win.DrawMenuBar(hWnd)
}
