package main

import (
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/windows"
)

// The icon draws, at every taskbar size, with see-through corners and an opaque middle.
// ALIBI_ICON_PREVIEW=<folder> also saves each one, enlarged, as a PNG to look at.
func TestTrayIcon(t *testing.T) {
	preview := os.Getenv("ALIBI_ICON_PREVIEW")
	for _, c := range []struct {
		text string
		v    verdict
	}{{"TJ", fine}, {"IR", wrong}, {"—", unknown}, {"MW", fine}} {
		for _, size := range []int{16, 20, 24, 32} {
			bitmap, pixels, err := drawIcon(c.text, c.v, size)
			if err != nil {
				t.Fatal(err)
			}
			if a := pixels[0] >> 24; a != 0 {
				t.Errorf("%s %d: corner alpha %d, want 0", c.text, size, a)
			}
			if a := pixels[size/2*size+1] >> 24; a != 255 {
				t.Errorf("%s %d: edge-middle alpha %d, want 255", c.text, size, a)
			}
			white := 0
			for _, p := range pixels {
				if p>>16&0xff > 0xe0 && p>>8&0xff > 0xe0 && p&0xff > 0xe0 {
					white++
				}
			}
			if white < size/2 {
				t.Errorf("%s %d: only %d white pixels: no text drawn?", c.text, size, white)
			}
			if preview != "" && size == 32 {
				img := image.NewNRGBA(image.Rect(0, 0, size*8, size*8))
				for y := range size * 8 {
					for x := range size * 8 {
						p := pixels[(y/8)*size+x/8]
						a := p >> 24
						un := func(v uint32) uint8 {
							if a == 0 {
								return 0
							}
							return uint8(min(255, v*255/a))
						}
						img.Set(x, y, color.NRGBA{un(p >> 16 & 0xff), un(p >> 8 & 0xff), un(p & 0xff), uint8(a)})
					}
				}
				f, _ := os.Create(filepath.Join(preview, c.text+".png"))
				png.Encode(f, img)
				f.Close()
			}
			procDeleteObject.Call(bitmap)
			icon, err := trayIcon(c.text, c.v, size)
			if err != nil || icon == 0 {
				t.Fatalf("trayIcon: %v", err)
			}
			procDestroyIcon.Call(icon)
		}
	}
}

// Drawing the icon again and again, as every redraw does, leaves no GDI object behind: a font
// deleted while still selected into the DC used to leak one each time.
func TestTrayIconLeaksNothing(t *testing.T) {
	getGuiResources := user32.NewProc("GetGuiResources")
	gdiObjects := func() int {
		n, _, _ := getGuiResources.Call(uintptr(windows.CurrentProcess()), 0) // GR_GDIOBJECTS
		return int(n)
	}
	// Wide text at small sizes takes several fonts before one fits.
	draw := func() {
		for _, text := range []string{"TJ", "MW", "WWW"} {
			for _, size := range []int{16, 32} {
				icon, err := trayIcon(text, fine, size)
				if err != nil {
					t.Fatal(err)
				}
				procDestroyIcon.Call(icon)
			}
		}
	}
	draw() // anything GDI sets up once, the first time
	before := gdiObjects()
	for range 100 {
		draw()
	}
	if after := gdiObjects(); after > before+2 {
		t.Errorf("%d GDI objects after 100 icons, %d before", after, before)
	}
}
