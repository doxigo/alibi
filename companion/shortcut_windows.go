package main

import (
	"os"
	"path/filepath"
	"runtime"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// The Start menu shortcut, saved the way Explorer saves one: through the shell's own IShellLink.

var (
	ole32                = windows.NewLazySystemDLL("ole32.dll")
	procCoCreateInstance = ole32.NewProc("CoCreateInstance")

	clsidShellLink = windows.GUID{Data1: 0x00021401, Data4: [8]byte{0xc0, 0, 0, 0, 0, 0, 0, 0x46}}
	iidShellLinkW  = windows.GUID{Data1: 0x000214f9, Data4: [8]byte{0xc0, 0, 0, 0, 0, 0, 0, 0x46}}
	iidPersistFile = windows.GUID{Data1: 0x0000010b, Data4: [8]byte{0xc0, 0, 0, 0, 0, 0, 0, 0x46}}
)

// Where each method sits in its interface's table.
const (
	comQueryInterface = 0
	comRelease        = 2

	linkGetPath             = 3 // IShellLinkW
	linkSetDescription      = 7
	linkSetWorkingDirectory = 9
	linkGetArguments        = 10
	linkSetArguments        = 11
	linkSetPath             = 20

	fileLoad = 5 // IPersistFile
	fileSave = 6
)

func startMenuShortcut() func(path, target, args, description string) error { return makeShortcut }

// comObject is a COM interface: a pointer to its table of methods.
type comObject struct{ methods *[32]uintptr }

func (o *comObject) call(method int, args ...uintptr) error {
	hr, _, _ := syscall.SyscallN(o.methods[method], append([]uintptr{uintptr(unsafe.Pointer(o))}, args...)...)
	if int32(hr) < 0 {
		return syscall.Errno(hr)
	}
	return nil
}

func (o *comObject) release() { o.call(comRelease) }

// withCOM runs f on one thread with COM set up there, as the shell's objects need.
func withCOM(f func() error) error {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	switch err := windows.CoInitializeEx(0, windows.COINIT_APARTMENTTHREADED); err {
	case nil, syscall.Errno(windows.S_FALSE):
		defer windows.CoUninitialize()
	case syscall.Errno(windows.RPC_E_CHANGED_MODE):
		// Set up the other way on this thread already, which does as well.
	default:
		return err
	}
	return f()
}

// newShellLink returns a shell link and its IPersistFile, which loads and saves it.
func newShellLink() (link, file *comObject, err error) {
	hr, _, _ := procCoCreateInstance.Call(uintptr(unsafe.Pointer(&clsidShellLink)), 0, windows.CLSCTX_INPROC_SERVER,
		uintptr(unsafe.Pointer(&iidShellLinkW)), uintptr(unsafe.Pointer(&link)))
	if int32(hr) < 0 {
		return nil, nil, syscall.Errno(hr)
	}
	if err := link.call(comQueryInterface, uintptr(unsafe.Pointer(&iidPersistFile)), uintptr(unsafe.Pointer(&file))); err != nil {
		link.release()
		return nil, nil, err
	}
	return link, file, nil
}

// makeShortcut saves a shortcut at path that starts target with args, in target's folder.
func makeShortcut(path, target, args, description string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return withCOM(func() error {
		link, file, err := newShellLink()
		if err != nil {
			return err
		}
		defer link.release()
		defer file.release()
		for _, set := range []struct {
			method int
			value  string
		}{{linkSetPath, target}, {linkSetArguments, args}, {linkSetDescription, description}, {linkSetWorkingDirectory, filepath.Dir(target)}} {
			p := utf16Ptr(set.value)
			err := link.call(set.method, uintptr(unsafe.Pointer(p)))
			runtime.KeepAlive(p)
			if err != nil {
				return err
			}
		}
		p := utf16Ptr(path)
		defer runtime.KeepAlive(p)
		return file.call(fileSave, uintptr(unsafe.Pointer(p)), 1)
	})
}
