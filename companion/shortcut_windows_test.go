package main

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"unsafe"

	"golang.org/x/sys/windows"
)

// The Start menu shortcut is a real one: the shell reads back the program it starts, and with what.
func TestShortcutStartsTheAgent(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "Alibi Companion", "alibi-companion-agent.exe")
	os.MkdirAll(filepath.Dir(target), 0o755)
	os.WriteFile(target, []byte("program"), 0o755)
	path := filepath.Join(dir, "Start Menu", "Programs", "Alibi Companion.lnk")
	if err := makeShortcut(path, target, "agent", "Alibi Companion"); err != nil {
		t.Fatal(err)
	}
	got, args, err := readShortcut(path)
	if err != nil {
		t.Fatal(err)
	}
	if !samePath(got, target) || args != "agent" {
		t.Errorf("the shortcut starts %q %q, want %q agent", got, args, target)
	}
}

func readShortcut(path string) (target, args string, err error) {
	err = withCOM(func() error {
		link, file, err := newShellLink()
		if err != nil {
			return err
		}
		defer link.release()
		defer file.release()
		p := utf16Ptr(path)
		err = file.call(fileLoad, uintptr(unsafe.Pointer(p)), 0) // STGM_READ
		runtime.KeepAlive(p)
		if err != nil {
			return err
		}
		buf := make([]uint16, windows.MAX_LONG_PATH)
		if err := link.call(linkGetPath, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)), 0, 0); err != nil {
			return err
		}
		target = windows.UTF16ToString(buf)
		if err := link.call(linkGetArguments, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf))); err != nil {
			return err
		}
		args = windows.UTF16ToString(buf)
		return nil
	})
	return target, args, err
}
