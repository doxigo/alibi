package main

import (
	"encoding/binary"
	"errors"
)

// guiSubsystem returns a Windows program, given as its bytes, marked as a windowed program rather
// than a console one, as `editbin /subsystem:windows` does: the same code, one header field apart.
// Windows then starts it at sign-in with no console window. The command line, and the browser
// starting the host, keep the console program.
func guiSubsystem(program []byte) ([]byte, error) {
	const (
		peSignature = 0x00004550 // "PE\0\0"
		coffHeader  = 20
		subsystem   = 68 // its offset in the optional header, the same in PE32 and PE32+
		console     = 3  // IMAGE_SUBSYSTEM_WINDOWS_CUI
		windowed    = 2  // IMAGE_SUBSYSTEM_WINDOWS_GUI
	)
	if len(program) < 0x40 || program[0] != 'M' || program[1] != 'Z' {
		return nil, errors.New("not a Windows program")
	}
	pe := int(binary.LittleEndian.Uint32(program[0x3c:]))
	at := pe + 4 + coffHeader + subsystem
	if pe < 0x40 || at+2 > len(program) || binary.LittleEndian.Uint32(program[pe:]) != peSignature {
		return nil, errors.New("not a Windows program")
	}
	switch magic := binary.LittleEndian.Uint16(program[pe+4+coffHeader:]); magic {
	case 0x10b, 0x20b: // PE32, PE32+
	default:
		return nil, errors.New("not a Windows program")
	}
	switch binary.LittleEndian.Uint16(program[at:]) {
	case windowed:
		return program, nil
	case console:
	default:
		return nil, errors.New("not a console program")
	}
	out := append([]byte(nil), program...)
	binary.LittleEndian.PutUint16(out[at:], windowed)
	return out, nil
}
