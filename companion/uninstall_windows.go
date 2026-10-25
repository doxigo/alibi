package main

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"golang.org/x/sys/windows"
)

type windowsAutoTZ struct{}

func autoTimeZoneSwitch() autoTimeZoneControl { return windowsAutoTZ{} }
func (windowsAutoTZ) on() bool                { return autoTimeZone() }
func (windowsAutoTZ) openSettings()           { openDateTimeSettings() }

// consoleAsk asks on the console, when there is one to type into.
func consoleAsk() func(string) bool {
	var mode uint32
	if windows.GetConsoleMode(windows.Handle(os.Stdin.Fd()), &mode) != nil {
		return nil // stdin isn't a console: piped, or started without one
	}
	in := bufio.NewReader(os.Stdin)
	return func(question string) bool {
		fmt.Print(question)
		answer, _ := in.ReadString('\n')
		answer = strings.ToLower(strings.TrimSpace(answer))
		return answer == "" || answer == "y" || answer == "yes"
	}
}

// handOffUninstall: Windows can't delete a program that's running, so when uninstall runs from the
// installed copy itself (Settings > Apps starts it that way), a copy in the temp folder takes over
// once this one has exited, and the whole folder can go. Windows deletes the copy at the next sign-in.
func handOffUninstall(sys system, quiet bool) (bool, error) {
	l, err := currentLayout(sys)
	if err != nil {
		return false, nil
	}
	self, err := sys.self()
	if err != nil || !samePath(self, l.exe) {
		return false, nil
	}
	program, err := os.ReadFile(self)
	if err != nil {
		return false, err
	}
	copy := filepath.Join(os.TempDir(), fmt.Sprintf("alibi-companion-uninstall-%d.exe", os.Getpid()))
	if err := os.WriteFile(copy, program, 0o755); err != nil {
		return false, err
	}
	deleteAtSignIn(sys.registry, copy) // or else the temp folder's own clean-up does, in time
	args := []string{"uninstall", "--after", strconv.Itoa(os.Getpid())}
	if quiet {
		args = append(args, "--quiet")
	}
	cmd := exec.Command(copy, args...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr // the same console window
	if err := cmd.Start(); err != nil {
		return false, err
	}
	cmd.Process.Release()
	return true, nil
}

// Windows runs each RunOnce entry once, at the next sign-in, and removes it.
const runOnceKey = `Software\Microsoft\Windows\CurrentVersion\RunOnce`

// deleteAtSignIn has Windows delete a file at the next sign-in. It uses advpack's DelNode, as IExpress
// installers do to clean up after themselves, because it shows no console window, unlike cmd's del.
func deleteAtSignIn(reg registry, path string) error {
	system32, err := windows.GetSystemDirectory()
	if err != nil {
		return err
	}
	command := fmt.Sprintf(`"%s" advpack.dll,DelNodeRunDLL32 "%s"`, filepath.Join(system32, "rundll32.exe"), path)
	return reg.setValue(runOnceKey, "Delete "+filepath.Base(path), command)
}

// waitForExit waits, up to 15 seconds, for the process that handed the uninstall over to exit.
func waitForExit(pid int) {
	h, err := windows.OpenProcess(windows.SYNCHRONIZE, false, uint32(pid))
	if err != nil {
		return // gone already
	}
	defer windows.CloseHandle(h)
	windows.WaitForSingleObject(h, 15_000)
}
