package main

import (
	"errors"
	"math"
	"unsafe"
)

var procGetTextExtent = gdi32.NewProc("GetTextExtentPoint32W")

// The stamp colours of Alibi's own design (DESIGN.md): airmail blue while the connection is where
// it should be, airmail red when it isn't, and ink grey before the first answer.
func stampColour(v verdict) (r, g, b uint32) {
	switch v {
	case fine:
		return 0x1f, 0x3f, 0x91
	case wrong:
		return 0xc4, 0x1e, 0x2a
	}
	return 0x63, 0x6b, 0x7c
}

// drawIcon draws the tray icon into a size×size 32-bit bitmap: text in white on a rounded stamp.
// It returns the bitmap (premultiplied BGRA, top row first) and its pixels.
func drawIcon(text string, v verdict, size int) (bitmap uintptr, pixels []uint32, err error) {
	dc, _, _ := procCreateCompatibleDC.Call(0)
	if dc == 0 {
		return 0, nil, errors.New("CreateCompatibleDC failed")
	}
	defer procDeleteDC.Call(dc)
	header := bitmapInfoHeader{Size: 40, Width: int32(size), Height: -int32(size), Planes: 1, BitCount: 32}
	var bits unsafe.Pointer // memory GDI owns, freed with the bitmap
	bitmap, _, _ = procCreateDIBSection.Call(dc, uintptr(unsafe.Pointer(&header)), 0, uintptr(unsafe.Pointer(&bits)), 0, 0)
	if bitmap == 0 || bits == nil {
		return 0, nil, errors.New("CreateDIBSection failed")
	}
	pixels = unsafe.Slice((*uint32)(bits), size*size)

	// The stamp: a rounded square, its corners smoothed by sampling each pixel 4×4.
	r, g, b := stampColour(v)
	cover := make([]float64, size*size)
	radius := float64(size) * 0.24
	inside := func(x, y float64) bool {
		cx := math.Max(radius, math.Min(float64(size)-radius, x))
		cy := math.Max(radius, math.Min(float64(size)-radius, y))
		return (x-cx)*(x-cx)+(y-cy)*(y-cy) <= radius*radius
	}
	for y := range size {
		for x := range size {
			n := 0
			for sy := range 4 {
				for sx := range 4 {
					if inside(float64(x)+(float64(sx)+0.5)/4, float64(y)+(float64(sy)+0.5)/4) {
						n++
					}
				}
			}
			cover[y*size+x] = float64(n) / 16
			pixels[y*size+x] = r<<16 | g<<8 | b
		}
	}

	old, _, _ := procSelectObject.Call(dc, bitmap)
	defer procSelectObject.Call(dc, old)
	// The largest bold Segoe UI that fits, one pixel clear of each side.
	label := utf16Ptr(text)
	length := uintptr(len([]rune(text)))
	var font uintptr
	for height := int(float64(size) * 0.72); height >= 6; height-- {
		if font != 0 {
			procDeleteObject.Call(font)
		}
		font, _, _ = procCreateFont.Call(uintptr(-int32(height)), 0, 0, 0, fwBold, 0, 0, 0, defaultCharset, 0, 0, antialiasedQuality, 0, uintptr(unsafe.Pointer(utf16Ptr("Segoe UI"))))
		procSelectObject.Call(dc, font)
		var extent struct{ CX, CY int32 }
		procGetTextExtent.Call(dc, uintptr(unsafe.Pointer(label)), length, uintptr(unsafe.Pointer(&extent)))
		if int(extent.CX) <= size-2 {
			break
		}
	}
	if font != 0 {
		defer procDeleteObject.Call(font)
	}
	procSetBkMode.Call(dc, bkTransparent)
	procSetTextColor.Call(dc, 0x00ffffff)
	area := rect{0, 0, int32(size), int32(size)}
	procDrawText.Call(dc, uintptr(unsafe.Pointer(label)), ^uintptr(0), uintptr(unsafe.Pointer(&area)), dtCenter|dtVCenter|dtSingleLine)
	procGdiFlush.Call()

	// GDI leaves alpha at zero. Give every pixel the stamp's coverage, premultiplied as Windows wants.
	for i, p := range pixels {
		c := cover[i]
		pr := uint32(float64(p>>16&0xff) * c)
		pg := uint32(float64(p>>8&0xff) * c)
		pb := uint32(float64(p&0xff) * c)
		pixels[i] = uint32(c*255+0.5)<<24 | pr<<16 | pg<<8 | pb
	}
	return bitmap, pixels, nil
}

// trayIcon makes the tray's icon for text and verdict at size pixels. DestroyIcon frees it.
func trayIcon(text string, v verdict, size int) (uintptr, error) {
	bitmap, _, err := drawIcon(text, v, size)
	if err != nil {
		return 0, err
	}
	defer procDeleteObject.Call(bitmap)
	mask, _, _ := procCreateBitmap.Call(uintptr(size), uintptr(size), 1, 1, 0)
	defer procDeleteObject.Call(mask)
	info := iconInfo{IsIcon: 1, Mask: mask, Color: bitmap}
	icon, _, _ := procCreateIconIndirect.Call(uintptr(unsafe.Pointer(&info)))
	if icon == 0 {
		return 0, errors.New("CreateIconIndirect failed")
	}
	return icon, nil
}
