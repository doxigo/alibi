// Command winres writes the resources the Go linker builds into the Windows programs: Alibi's stamp
// as their icon (from the extension's own img/icon-*.png), their version information, and a
// manifest. With them, Windows shows "Alibi Companion" and the stamp in Settings > Apps, in
// Explorer, and on the tray app's notifications. scripts/build-companion.mjs runs it:
//
//	go run ./cmd/winres -version 1.1.0 -arch amd64 -o rsrc_windows_amd64.syso icon-16.png icon-32.png …
//
// The output is a COFF object holding a .rsrc section, as `rc` and `cvtres` would make it.
package main

import (
	"bytes"
	"encoding/binary"
	"flag"
	"fmt"
	"image"
	"image/png"
	"log"
	"os"
	"slices"
	"strconv"
	"strings"
	"unicode/utf16"
)

const (
	rtIcon      = 3
	rtGroupIcon = 14
	rtVersion   = 16
	rtManifest  = 24
	langEnUS    = 0x0409
)

type resource struct {
	typ, id uint16
	data    []byte
}

func main() {
	version := flag.String("version", "0.0.0", "the program's version, as in manifest.json")
	arch := flag.String("arch", "amd64", "amd64 or arm64")
	out := flag.String("o", "", "the .syso file to write")
	flag.Parse()
	if *out == "" || flag.NArg() == 0 {
		log.Fatal("usage: winres -version 1.1.0 -arch amd64 -o rsrc_windows_amd64.syso icon.png…")
	}
	var resources []resource
	var group []byte
	group = binary.LittleEndian.AppendUint16(group, 0) // reserved
	group = binary.LittleEndian.AppendUint16(group, 1) // icons
	group = binary.LittleEndian.AppendUint16(group, uint16(flag.NArg()))
	for i, path := range flag.Args() {
		icon, size, err := iconImage(path)
		if err != nil {
			log.Fatalf("%s: %v", path, err)
		}
		id := uint16(i + 1)
		resources = append(resources, resource{rtIcon, id, icon})
		dim := byte(size)
		if size >= 256 {
			dim = 0
		}
		group = append(group, dim, dim, 0, 0)
		group = binary.LittleEndian.AppendUint16(group, 1)  // planes
		group = binary.LittleEndian.AppendUint16(group, 32) // bits per pixel
		group = binary.LittleEndian.AppendUint32(group, uint32(len(icon)))
		group = binary.LittleEndian.AppendUint16(group, id)
	}
	resources = append(resources,
		resource{rtGroupIcon, 1, group}, // id 1: the program's icon
		resource{rtVersion, 1, versionInfo(*version)},
		resource{rtManifest, 1, []byte(manifest(*version))},
	)
	// The COFF machine, and its relocation for an address relative to the program's base.
	target, ok := map[string]struct{ machine, relocation uint16 }{
		"amd64": {0x8664, 3}, // IMAGE_REL_AMD64_ADDR32NB
		"arm64": {0xaa64, 2}, // IMAGE_REL_ARM64_ADDR32NB
	}[*arch]
	if !ok {
		log.Fatalf("unknown arch %s", *arch)
	}
	if err := os.WriteFile(*out, coff(resources, target.machine, target.relocation), 0o644); err != nil {
		log.Fatal(err)
	}
}

// iconImage turns a square PNG into an icon image: a 32-bit DIB with its height doubled, rows
// bottom-up, then a 1-bit mask that's set where the image is fully transparent.
func iconImage(path string) ([]byte, int, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, 0, err
	}
	defer f.Close()
	img, err := png.Decode(f)
	if err != nil {
		return nil, 0, err
	}
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	if w != h || w > 256 {
		return nil, 0, fmt.Errorf("%dx%d: an icon must be square, at most 256 pixels", w, h)
	}
	nrgba := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			nrgba.Set(x, y, img.At(b.Min.X+x, b.Min.Y+y))
		}
	}
	maskRow := (w + 31) / 32 * 4
	var out []byte
	for _, v := range []uint32{40, uint32(w), uint32(2 * h)} {
		out = binary.LittleEndian.AppendUint32(out, v)
	}
	out = binary.LittleEndian.AppendUint16(out, 1)  // planes
	out = binary.LittleEndian.AppendUint16(out, 32) // bits per pixel
	for _, v := range []uint32{0, uint32(w*h*4 + maskRow*h), 0, 0, 0, 0} {
		out = binary.LittleEndian.AppendUint32(out, v)
	}
	for y := h - 1; y >= 0; y-- {
		for x := range w {
			c := nrgba.NRGBAAt(x, y)
			out = append(out, c.B, c.G, c.R, c.A)
		}
	}
	for y := h - 1; y >= 0; y-- {
		row := make([]byte, maskRow)
		for x := range w {
			if nrgba.NRGBAAt(x, y).A == 0 {
				row[x/8] |= 0x80 >> (x % 8)
			}
		}
		out = append(out, row...)
	}
	return out, w, nil
}

// versionInfo is a VS_VERSIONINFO block: the numbers, then the strings Windows shows.
func versionInfo(version string) []byte {
	parts := strings.Split(strings.SplitN(version, "-", 2)[0], ".")
	num := [4]uint16{}
	for i := 0; i < len(parts) && i < 4; i++ {
		n, _ := strconv.Atoi(parts[i])
		num[i] = uint16(n)
	}
	ms, ls := uint32(num[0])<<16|uint32(num[1]), uint32(num[2])<<16|uint32(num[3])
	var fixed []byte
	for _, v := range []uint32{0xfeef04bd, 0x00010000, ms, ls, ms, ls, 0x3f, 0, 0x00040004, 1, 0, 0, 0} {
		fixed = binary.LittleEndian.AppendUint32(fixed, v)
	}
	var strs [][]byte
	for _, kv := range [][2]string{
		{"CompanyName", "Alibi"},
		{"FileDescription", "Alibi Companion"},
		{"FileVersion", version},
		{"InternalName", "alibi-companion"},
		{"LegalCopyright", "GPL-3.0 · github.com/doxigo/alibi"},
		{"OriginalFilename", "alibi-companion.exe"},
		{"ProductName", "Alibi Companion"},
		{"ProductVersion", version},
	} {
		value := utf16z(kv[1])
		strs = append(strs, block(kv[0], 1, value, uint16(len(value)/2), nil))
	}
	text := block("StringFileInfo", 1, nil, 0, [][]byte{block("040904B0", 1, nil, 0, strs)})
	translation := binary.LittleEndian.AppendUint32(nil, 0x04b00409) // US English, Unicode
	vars := block("VarFileInfo", 1, nil, 0, [][]byte{block("Translation", 0, translation, 4, nil)})
	return block("VS_VERSION_INFO", 0, fixed, uint16(len(fixed)), [][]byte{text, vars})
}

// block is one node of a version resource: its length, the value's length, its type (1 text,
// 0 binary), its key, then the value and its children, each starting on a 4-byte boundary.
func block(key string, typ uint16, value []byte, valueLength uint16, children [][]byte) []byte {
	b := make([]byte, 6)
	binary.LittleEndian.PutUint16(b[2:], valueLength)
	binary.LittleEndian.PutUint16(b[4:], typ)
	b = append(b, utf16z(key)...)
	b = pad4(b)
	b = append(b, value...)
	for _, child := range children {
		b = pad4(b)
		b = append(b, child...)
	}
	binary.LittleEndian.PutUint16(b, uint16(len(b)))
	return b
}

func pad4(b []byte) []byte {
	for len(b)%4 != 0 {
		b = append(b, 0)
	}
	return b
}

func utf16z(s string) []byte {
	var b []byte
	for _, u := range utf16.Encode([]rune(s)) {
		b = binary.LittleEndian.AppendUint16(b, u)
	}
	return append(b, 0, 0)
}

// manifest says the program runs as the user who starts it, never asking to elevate; that it's
// made for Windows 10 and 11; that it's sharp at every display scale; and that it wants today's
// menus and message boxes.
func manifest(version string) string {
	numbers := strings.Split(strings.SplitN(version, "-", 2)[0], ".")
	for len(numbers) < 4 {
		numbers = append(numbers, "0")
	}
	return `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<assembly xmlns="urn:schemas-microsoft-com:asm.v1" manifestVersion="1.0">
  <assemblyIdentity type="win32" name="Alibi.Companion" version="` + strings.Join(numbers[:4], ".") + `"/>
  <description>Alibi Companion</description>
  <trustInfo xmlns="urn:schemas-microsoft-com:asm.v3">
    <security>
      <requestedPrivileges>
        <requestedExecutionLevel level="asInvoker" uiAccess="false"/>
      </requestedPrivileges>
    </security>
  </trustInfo>
  <compatibility xmlns="urn:schemas-microsoft-com:compatibility.v1">
    <application>
      <supportedOS Id="{8e0f7a12-bfb3-4fe8-b9a5-48fd50a15a9a}"/>
    </application>
  </compatibility>
  <application xmlns="urn:schemas-microsoft-com:asm.v3">
    <windowsSettings>
      <dpiAware xmlns="http://schemas.microsoft.com/SMI/2005/WindowsSettings">true/pm</dpiAware>
      <dpiAwareness xmlns="http://schemas.microsoft.com/SMI/2016/WindowsSettings">PerMonitorV2</dpiAwareness>
    </windowsSettings>
  </application>
  <dependency>
    <dependentAssembly>
      <assemblyIdentity type="win32" name="Microsoft.Windows.Common-Controls" version="6.0.0.0" processorArchitecture="*" publicKeyToken="6595b64144ccf1df" language="*"/>
    </dependentAssembly>
  </dependency>
</assembly>
`
}

// coff lays the resources out as a resource directory (type, then id, then language) in a .rsrc
// section, with a relocation for each data entry, so the linker can point them at the section.
func coff(resources []resource, machine, relocationType uint16) []byte {
	slices.SortFunc(resources, func(a, b resource) int {
		if a.typ != b.typ {
			return int(a.typ) - int(b.typ)
		}
		return int(a.id) - int(b.id)
	})
	var types []uint16
	byType := map[uint16][]int{}
	for i, r := range resources {
		if len(byType[r.typ]) == 0 {
			types = append(types, r.typ)
		}
		byType[r.typ] = append(byType[r.typ], i)
	}
	// Where everything goes: the root, each type's directory, each resource's language directory,
	// each resource's data entry, then the data itself.
	offset := 16 + 8*len(types)
	typeDir := map[uint16]int{}
	for _, t := range types {
		typeDir[t] = offset
		offset += 16 + 8*len(byType[t])
	}
	langDir := make([]int, len(resources))
	for i := range resources {
		langDir[i] = offset
		offset += 16 + 8
	}
	dataEntry := make([]int, len(resources))
	for i := range resources {
		dataEntry[i] = offset
		offset += 16
	}
	dataAt := make([]int, len(resources))
	for i, r := range resources {
		offset = (offset + 7) &^ 7
		dataAt[i] = offset
		offset += len(r.data)
	}
	section := make([]byte, (offset+7)&^7)
	put16 := func(at int, v uint16) { binary.LittleEndian.PutUint16(section[at:], v) }
	put32 := func(at int, v uint32) { binary.LittleEndian.PutUint32(section[at:], v) }
	directory := func(at, entries int) { put16(at+14, uint16(entries)) } // all entries by id
	const subdirectory = 0x80000000

	directory(0, len(types))
	for i, t := range types {
		put32(16+8*i, uint32(t))
		put32(16+8*i+4, subdirectory|uint32(typeDir[t]))
		directory(typeDir[t], len(byType[t]))
		for j, r := range byType[t] {
			put32(typeDir[t]+16+8*j, uint32(resources[r].id))
			put32(typeDir[t]+16+8*j+4, subdirectory|uint32(langDir[r]))
		}
	}
	for i, r := range resources {
		directory(langDir[i], 1)
		put32(langDir[i]+16, langEnUS)
		put32(langDir[i]+20, uint32(dataEntry[i]))
		put32(dataEntry[i], uint32(dataAt[i])) // relocated: becomes the data's address in the program
		put32(dataEntry[i]+4, uint32(len(r.data)))
		copy(section[dataAt[i]:], r.data)
	}

	var b bytes.Buffer
	w := func(v any) { binary.Write(&b, binary.LittleEndian, v) }
	const headers = 20 + 40
	relocations := headers + len(section)
	symbols := relocations + 10*len(resources)
	// File header.
	w(machine)
	w(uint16(1)) // sections
	w(uint32(0)) // time stamp
	w(uint32(symbols))
	w(uint32(1)) // symbols
	w(uint16(0)) // optional header size
	w(uint16(0)) // characteristics
	// Section header.
	b.WriteString(".rsrc\x00\x00\x00")
	w(uint32(0)) // virtual size
	w(uint32(0)) // virtual address
	w(uint32(len(section)))
	w(uint32(headers))
	w(uint32(relocations))
	w(uint32(0)) // line numbers
	w(uint16(len(resources)))
	w(uint16(0))
	w(uint32(0x40000040)) // initialized data, readable
	b.Write(section)
	for i := range resources {
		w(uint32(dataEntry[i]))
		w(uint32(0)) // symbol 0: the section itself
		w(relocationType)
	}
	// The section's symbol, then an empty string table.
	b.WriteString(".rsrc\x00\x00\x00")
	w(uint32(0))
	w(int16(1)) // section 1
	w(uint16(0))
	w(uint8(3)) // static
	w(uint8(0))
	w(uint32(4))
	return b.Bytes()
}
