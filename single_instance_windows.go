//go:build windows

package main

import (
	"errors"
	"fmt"

	"github.com/lxn/win"
	"golang.org/x/sys/windows"
)

const singleInstanceMutexName = `Local\Ros.SingleInstance`

var errSingleInstanceAlreadyRunning = errors.New("single instance already running")

func acquireSingleInstanceMutex() (windows.Handle, error) {
	name, err := windows.UTF16PtrFromString(singleInstanceMutexName)
	if err != nil {
		return 0, fmt.Errorf("invalid mutex name: %w", err)
	}

	handle, err := windows.CreateMutex(nil, false, name)
	if err != nil {
		if handle != 0 {
			_ = windows.CloseHandle(handle)
		}
		if errors.Is(err, windows.ERROR_ALREADY_EXISTS) {
			return 0, errSingleInstanceAlreadyRunning
		}
		return 0, fmt.Errorf("create single-instance mutex failed: %w", err)
	}

	return handle, nil
}

func releaseSingleInstanceMutex(handle windows.Handle) {
	if handle == 0 {
		return
	}
	_ = windows.CloseHandle(handle)
}

func showAlreadyRunningMessage() {
	text, textErr := windows.UTF16PtrFromString("Ros 已在运行。")
	caption, captionErr := windows.UTF16PtrFromString("提示")
	if textErr != nil || captionErr != nil {
		return
	}

	win.MessageBox(0, text, caption, win.MB_OK|win.MB_ICONINFORMATION)
}
