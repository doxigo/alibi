package main

import (
	"strings"
	"testing"

	"golang.org/x/sys/windows"
)

// A Windows without one of the functions time zone sync calls says so, rather than panicking the
// first time the agent reads or moves the zone.
func TestSyncNeedsEveryFunction(t *testing.T) {
	missing := windows.NewLazySystemDLL("icu.dll").NewProc("ucal_noSuchFunction")
	err := allFound(ucalWindowsZone, missing)
	if err == nil || !strings.Contains(err.Error(), "Windows 10 version 1903 or later") {
		t.Errorf("err = %v", err)
	}
	gone := windows.NewLazySystemDLL("no-such-library.dll").NewProc("anything")
	if allFound(gone) == nil {
		t.Error("a missing library counted as there")
	}
	// The Windows this runs on has them all, as every one since 1903 does.
	if err := syncSupported(); err != nil {
		t.Errorf("this Windows: %v", err)
	}
}

// One agent at a time keeps the machine's clock; another gets it once the first lets go.
func TestOneAgentKeepsTheClock(t *testing.T) {
	name := `Local\AlibiCompanionClockTest` + t.Name()
	first, second := &clockLock{name: name}, &clockLock{name: name}
	defer first.close()
	defer second.close()
	if !first.claim() {
		t.Fatal("the first agent didn't get the clock")
	}
	if second.claim() {
		t.Fatal("a second agent got the clock while the first had it")
	}
	if !first.claim() {
		t.Error("the first agent lost the clock by asking again")
	}
	first.close()
	if !second.claim() {
		t.Error("the second agent didn't get the clock once the first let go")
	}
}
