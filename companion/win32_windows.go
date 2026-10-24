package main

import (
	"unsafe"

	"golang.org/x/sys/windows"
)

// The slice of the Windows API the tray uses.

var (
	gdi32   = windows.NewLazySystemDLL("gdi32.dll")
	shell32 = windows.NewLazySystemDLL("shell32.dll")

	procRegisterClassEx        = user32.NewProc("RegisterClassExW")
	procCreateWindowEx         = user32.NewProc("CreateWindowExW")
	procDefWindowProc          = user32.NewProc("DefWindowProcW")
	procDestroyWindow          = user32.NewProc("DestroyWindow")
	procGetMessage             = user32.NewProc("GetMessageW")
	procTranslateMessage       = user32.NewProc("TranslateMessage")
	procDispatchMessage        = user32.NewProc("DispatchMessageW")
	procPostMessage            = user32.NewProc("PostMessageW")
	procPostQuitMessage        = user32.NewProc("PostQuitMessage")
	procCreatePopupMenu        = user32.NewProc("CreatePopupMenu")
	procAppendMenu             = user32.NewProc("AppendMenuW")
	procSetMenuDefaultItem     = user32.NewProc("SetMenuDefaultItem")
	procTrackPopupMenuEx       = user32.NewProc("TrackPopupMenuEx")
	procDestroyMenu            = user32.NewProc("DestroyMenu")
	procSetForegroundWindow    = user32.NewProc("SetForegroundWindow")
	procGetCursorPos           = user32.NewProc("GetCursorPos")
	procRegisterWindowMessage  = user32.NewProc("RegisterWindowMessageW")
	procCreateIconIndirect     = user32.NewProc("CreateIconIndirect")
	procDestroyIcon            = user32.NewProc("DestroyIcon")
	procGetSystemMetricsForDpi = user32.NewProc("GetSystemMetricsForDpi")
	procGetSystemMetrics       = user32.NewProc("GetSystemMetrics")
	procGetDpiForSystem        = user32.NewProc("GetDpiForSystem")
	procSetDpiAwarenessContext = user32.NewProc("SetProcessDpiAwarenessContext")
	procMessageBeep            = user32.NewProc("MessageBeep")
	procShowWindow             = user32.NewProc("ShowWindow")
	procSetWindowPos           = user32.NewProc("SetWindowPos")
	procSetLayeredWindowAttrs  = user32.NewProc("SetLayeredWindowAttributes")
	procSetTimer               = user32.NewProc("SetTimer")
	procLoadIcon               = user32.NewProc("LoadIconW")
	procMessageBox             = user32.NewProc("MessageBoxW")

	procShellNotifyIcon = shell32.NewProc("Shell_NotifyIconW")
	procShellExecute    = shell32.NewProc("ShellExecuteW")

	procCreateCompatibleDC = gdi32.NewProc("CreateCompatibleDC")
	procCreateDIBSection   = gdi32.NewProc("CreateDIBSection")
	procCreateBitmap       = gdi32.NewProc("CreateBitmap")
	procSelectObject       = gdi32.NewProc("SelectObject")
	procDeleteObject       = gdi32.NewProc("DeleteObject")
	procDeleteDC           = gdi32.NewProc("DeleteDC")
	procCreateFont         = gdi32.NewProc("CreateFontW")
	procSetBkMode          = gdi32.NewProc("SetBkMode")
	procSetTextColor       = gdi32.NewProc("SetTextColor")
	procDrawText           = user32.NewProc("DrawTextW")
	procCreateSolidBrush   = gdi32.NewProc("CreateSolidBrush")
	procGdiFlush           = gdi32.NewProc("GdiFlush")
)

const (
	wmDestroy         = 0x0002
	wmClose           = 0x0010
	wmQueryEndSession = 0x0011
	wmEndSession      = 0x0016
	wmTimeChange      = 0x001e
	wmCommand         = 0x0111
	wmTimer           = 0x0113
	wmContextMenu     = 0x007b
	wmLButtonUp       = 0x0202
	wmRButtonUp       = 0x0205
	wmDpiChanged      = 0x02e0
	wmUser            = 0x0400
	wmApp             = 0x8000

	ninSelect           = wmUser + 0
	ninKeySelect        = wmUser + 1
	ninBalloonUserClick = wmUser + 5

	nimAdd        = 0
	nimModify     = 1
	nimDelete     = 2
	nimSetVersion = 4

	nifMessage  = 0x01
	nifIcon     = 0x02
	nifTip      = 0x04
	nifInfo     = 0x10
	nifShowTip  = 0x80
	niifInfo    = 0x01
	niifWarning = 0x02
	niifNoSound = 0x10
	niifQuiet   = 0x80 // respect quiet time

	notifyIconVersion4 = 4

	mfString    = 0x0000
	mfGrayed    = 0x0001
	mfChecked   = 0x0008
	mfSeparator = 0x0800

	tpmRightButton = 0x0002
	tpmReturnCmd   = 0x0100
	tpmNoNotify    = 0x0080

	wsPopup         = 0x80000000
	wsExTopmost     = 0x00000008
	wsExTransparent = 0x00000020
	wsExToolWindow  = 0x00000080
	wsExLayered     = 0x00080000
	wsExNoActivate  = 0x08000000

	swHide           = 0
	swShowNormal     = 1
	swShowNoActivate = 4
	lwaAlpha         = 0x02
	hwndTopmost      = ^uintptr(0) // (HWND)-1
	swpNoActivate    = 0x0010

	smCxScreen = 0
	smCxSmIcon = 49

	fwBold             = 700
	antialiasedQuality = 4
	defaultCharset     = 1
	dtCenter           = 0x01
	dtVCenter          = 0x04
	dtSingleLine       = 0x20
	bkTransparent      = 1

	mbIconWarning     = 0x30
	mbIconInformation = 0x40
	mbOK              = 0x00
	mbYesNo           = 0x04
	idYes             = 6
)

// dpiAwarenessPerMonitorV2 is DPI_AWARENESS_CONTEXT_PER_MONITOR_AWARE_V2, (HANDLE)-4.
const dpiAwarenessPerMonitorV2 = ^uintptr(3)

type point struct{ X, Y int32 }

type rect struct{ Left, Top, Right, Bottom int32 }

type msg struct {
	Hwnd    uintptr
	Message uint32
	WParam  uintptr
	LParam  uintptr
	Time    uint32
	Pt      point
	Private uint32
}

type wndClassEx struct {
	Size       uint32
	Style      uint32
	WndProc    uintptr
	ClsExtra   int32
	WndExtra   int32
	Instance   uintptr
	Icon       uintptr
	Cursor     uintptr
	Background uintptr
	MenuName   *uint16
	ClassName  *uint16
	IconSm     uintptr
}

// notifyIconData is NOTIFYICONDATAW, 976 bytes on 64-bit Windows.
type notifyIconData struct {
	Size            uint32
	Wnd             uintptr
	ID              uint32
	Flags           uint32
	CallbackMessage uint32
	Icon            uintptr
	Tip             [128]uint16
	State           uint32
	StateMask       uint32
	Info            [256]uint16
	Version         uint32 // uTimeout and uVersion share this place
	InfoTitle       [64]uint16
	InfoFlags       uint32
	GuidItem        windows.GUID
	BalloonIcon     uintptr
}

type iconInfo struct {
	IsIcon   int32
	XHotspot uint32
	YHotspot uint32
	Mask     uintptr
	Color    uintptr
}

type bitmapInfoHeader struct {
	Size          uint32
	Width         int32
	Height        int32
	Planes        uint16
	BitCount      uint16
	Compression   uint32
	SizeImage     uint32
	XPelsPerMeter int32
	YPelsPerMeter int32
	ClrUsed       uint32
	ClrImportant  uint32
}

// copyUTF16 fills a fixed-size UTF-16 field, cut to fit and always terminated.
func copyUTF16(dst []uint16, s string) {
	u, _ := windows.UTF16FromString(s)
	if len(u) > len(dst) {
		u = append(u[:len(dst)-1], 0)
	}
	copy(dst, u)
	for i := len(u); i < len(dst); i++ {
		dst[i] = 0
	}
}

func utf16Ptr(s string) *uint16 {
	p, _ := windows.UTF16PtrFromString(s)
	return p
}

// shellOpen opens a file, folder or web address the way double-clicking it would.
func shellOpen(target string) {
	procShellExecute.Call(0, uintptr(unsafe.Pointer(utf16Ptr("open"))), uintptr(unsafe.Pointer(utf16Ptr(target))), 0, 0, swShowNormal)
}

// messageBox shows a plain Windows message box and reports whether Yes was chosen.
func messageBox(title, text string, flags uintptr) bool {
	r, _, _ := procMessageBox.Call(0, uintptr(unsafe.Pointer(utf16Ptr(text))), uintptr(unsafe.Pointer(utf16Ptr(title))), flags)
	return r == idYes
}
