package main

import (
	"testing"
	"unsafe"
)

// The structs Windows reads must have exactly its layout.
func TestWin32Layouts(t *testing.T) {
	if unsafe.Sizeof(uintptr(0)) != 8 {
		t.Skip("layouts checked for 64-bit Windows")
	}
	for name, c := range map[string][2]uintptr{
		"NOTIFYICONDATAW":          {unsafe.Sizeof(notifyIconData{}), 976},
		"MSG":                      {unsafe.Sizeof(msg{}), 48},
		"WNDCLASSEXW":              {unsafe.Sizeof(wndClassEx{}), 80},
		"ICONINFO":                 {unsafe.Sizeof(iconInfo{}), 32},
		"BITMAPINFOHEADER":         {unsafe.Sizeof(bitmapInfoHeader{}), 40},
		"NOTIFYICONDATAW.hIcon":    {unsafe.Offsetof(notifyIconData{}.Icon), 32},
		"NOTIFYICONDATAW.szInfo":   {unsafe.Offsetof(notifyIconData{}.Info), 304},
		"NOTIFYICONDATAW.guidItem": {unsafe.Offsetof(notifyIconData{}.GuidItem), 952},
	} {
		if c[0] != c[1] {
			t.Errorf("%s: %d, want %d", name, c[0], c[1])
		}
	}
}
