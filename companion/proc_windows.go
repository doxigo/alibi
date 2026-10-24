package main

import (
	"os"

	"golang.org/x/sys/windows"
)

// stillActive is the exit code Windows reports for a process that hasn't exited.
const stillActive = 259

func thisProcess() process {
	var created, exited, kernel, user windows.Filetime
	windows.GetProcessTimes(windows.CurrentProcess(), &created, &exited, &kernel, &user)
	return process{pid: os.Getpid(), started: created.Nanoseconds()}
}

// running reports whether the process still runs. Windows hands a number on to a new process soon
// after the old one exits, so the start time has to match too.
func (p process) running() bool {
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(p.pid))
	if err != nil {
		return false // gone, or another user's process that took its number
	}
	defer windows.CloseHandle(h)
	var code uint32
	if windows.GetExitCodeProcess(h, &code) != nil || code != stillActive {
		return false
	}
	var created, exited, kernel, user windows.Filetime
	if windows.GetProcessTimes(h, &created, &exited, &kernel, &user) != nil {
		return false
	}
	return created.Nanoseconds() == p.started
}
