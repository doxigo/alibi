//go:build !windows

package main

import (
	"errors"
	"io"
)

// Time zone sync is Windows-only for now. On Linux, setting the zone needs an administrator's
// approval (polkit) that a program starting at sign-in can't ask for.
var errNoAgent = errors.New("time zone sync runs on Windows only")

func agentControl() agentController               { return nil }
func autoTimeZoneSwitch() autoTimeZoneControl     { return nil }
func consoleAsk() func(string) bool               { return nil }
func handOffUninstall(system, bool) (bool, error) { return false, nil }
func waitForExit(int)                             {}
func systemZones() zoneSystem                     { return nil }
func clockNotesHere() clockNotes                  { return nil }
func autoTimeZone() bool                          { return false }

func agentCommand(system, []string, io.Writer) error { return errNoAgent }
