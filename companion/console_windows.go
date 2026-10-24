package main

import (
	"unsafe"

	"golang.org/x/sys/windows"
)

var getConsoleProcessList = windows.NewLazySystemDLL("kernel32.dll").NewProc("GetConsoleProcessList")

// startedFromExplorer reports whether the program has its console to itself, as it does when it's
// double-clicked, rather than sharing a terminal that stays open after it exits.
func startedFromExplorer() bool {
	processes := make([]uint32, 2)
	n, _, _ := getConsoleProcessList.Call(uintptr(unsafe.Pointer(&processes[0])), uintptr(len(processes)))
	return n == 1
}
