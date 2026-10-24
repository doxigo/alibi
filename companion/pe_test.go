package main

import (
	"bytes"
	"debug/pe"
	"encoding/binary"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// A Windows program's headers, as far as guiSubsystem reads them.
func fakeProgram(magic, subsystem uint16) []byte {
	b := make([]byte, 0x200)
	b[0], b[1] = 'M', 'Z'
	binary.LittleEndian.PutUint32(b[0x3c:], 0x80)
	binary.LittleEndian.PutUint32(b[0x80:], 0x4550)
	binary.LittleEndian.PutUint16(b[0x80+24:], magic)
	binary.LittleEndian.PutUint16(b[0x80+24+68:], subsystem)
	return b
}

func TestGUISubsystem(t *testing.T) {
	for _, magic := range []uint16{0x10b, 0x20b} {
		console := fakeProgram(magic, 3)
		before := bytes.Clone(console)
		windowed, err := guiSubsystem(console)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(console, before) {
			t.Error("changed the program it was given")
		}
		if !bytes.Equal(windowed, fakeProgram(magic, 2)) {
			t.Errorf("magic %#x: more changed than the subsystem", magic)
		}
		if again, _ := guiSubsystem(windowed); !bytes.Equal(again, windowed) {
			t.Error("a windowed program didn't stay as it was")
		}
	}
	for name, program := range map[string][]byte{
		"text":         []byte("program 1"),
		"short":        {'M', 'Z'},
		"native":       fakeProgram(0x20b, 1),
		"no PE":        append([]byte("MZ"), make([]byte, 0x100)...),
		"odd optional": fakeProgram(0x107, 3),
	} {
		if _, err := guiSubsystem(program); err == nil {
			t.Errorf("%s: no error", name)
		}
	}
}

// The real thing: this test program, as Windows sees it before and after.
func TestGUISubsystemOnThisProgram(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("only a Windows build is a Windows program")
	}
	self, _ := os.Executable()
	program, err := os.ReadFile(self)
	if err != nil {
		t.Fatal(err)
	}
	windowed, err := guiSubsystem(program)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "agent.exe")
	os.WriteFile(path, windowed, 0o755)
	for file, want := range map[string]uint16{self: pe.IMAGE_SUBSYSTEM_WINDOWS_CUI, path: pe.IMAGE_SUBSYSTEM_WINDOWS_GUI} {
		f, err := pe.Open(file)
		if err != nil {
			t.Fatal(err)
		}
		var got uint16
		switch h := f.OptionalHeader.(type) {
		case *pe.OptionalHeader64:
			got = h.Subsystem
		case *pe.OptionalHeader32:
			got = h.Subsystem
		}
		f.Close()
		if got != want {
			t.Errorf("%s: subsystem %d, want %d", filepath.Base(file), got, want)
		}
	}
}
