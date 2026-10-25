package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
)

// The uninstall copy left in the temp folder goes at the next sign-in: Windows runs the RunOnce
// entry, and that deletes it.
func TestTheUninstallCopyIsDeletedAtSignIn(t *testing.T) {
	copy := filepath.Join(t.TempDir(), "alibi-companion-uninstall-1234.exe")
	if err := os.WriteFile(copy, []byte("program"), 0o755); err != nil {
		t.Fatal(err)
	}
	reg := fakeRegistry{}
	if err := deleteAtSignIn(reg, copy); err != nil {
		t.Fatal(err)
	}
	command, found, _ := reg.value(runOnceKey, "Delete alibi-companion-uninstall-1234.exe")
	if !found {
		t.Fatalf("no RunOnce entry: %v", reg)
	}
	// Run it as Windows would at sign-in.
	run := exec.Command(filepath.Join(os.Getenv("SystemRoot"), "System32", "rundll32.exe"))
	run.SysProcAttr = &syscall.SysProcAttr{CmdLine: command}
	if err := run.Run(); err != nil {
		t.Fatalf("%s: %v", command, err)
	}
	if _, err := os.Stat(copy); !os.IsNotExist(err) {
		t.Errorf("%s didn't delete the copy", command)
	}
}
