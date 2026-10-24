//go:build !windows

package main

import (
	"os"
	"syscall"
)

func thisProcess() process { return process{pid: os.Getpid()} }

// running reports whether the process still runs.
func (p process) running() bool { return p.pid > 0 && syscall.Kill(p.pid, 0) == nil }
