package main

import (
	"errors"
	"fmt"
	"path/filepath"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Windows' "Set time zone automatically" moves the zone to where Windows' location service puts the
// computer, which is your real location, so it fights the time zone sync. It's a machine-wide
// setting (the tzautoupdate service), so changing it takes an administrator: these ask through
// Windows' own approval prompt, as the Settings app does.

const tzautoupdateKey = `HKLM\SYSTEM\CurrentControlSet\Services\tzautoupdate`

func turnOffAutoTimeZone(hwnd uintptr) error {
	return setAutoTimeZone(hwnd, false)
}

func turnOnAutoTimeZone(hwnd uintptr) error {
	return setAutoTimeZone(hwnd, true)
}

// setAutoTimeZone sets the service's start type the way the Settings switch does (3 on, 4 off),
// stops or starts it, and waits until Windows reports the change.
func setAutoTimeZone(hwnd uintptr, on bool) error {
	start, service := "4", "stop"
	if on {
		start, service = "3", "start"
	}
	dir, err := windows.GetSystemDirectory()
	if err != nil {
		return err
	}
	args := fmt.Sprintf(`/c reg add "%s" /v Start /t REG_DWORD /d %s /f & sc %s tzautoupdate`, tzautoupdateKey, start, service)
	r, _, _ := procShellExecute.Call(hwnd, uintptr(unsafe.Pointer(utf16Ptr("runas"))),
		uintptr(unsafe.Pointer(utf16Ptr(filepath.Join(dir, "cmd.exe")))), uintptr(unsafe.Pointer(utf16Ptr(args))), 0, swHide)
	if r <= 32 {
		return errors.New("Windows' automatic time zone stays as it is: the approval was declined or failed")
	}
	for range 50 {
		if autoTimeZone() == on {
			return nil
		}
		time.Sleep(200 * time.Millisecond)
	}
	return errors.New("Windows didn't change its automatic time zone setting")
}
