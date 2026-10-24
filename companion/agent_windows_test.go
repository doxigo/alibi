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
