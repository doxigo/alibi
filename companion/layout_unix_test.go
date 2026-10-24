//go:build !windows && !darwin

package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// linuxSystem is a user with a scratch home, and profile folders for the given browsers.
func linuxSystem(t *testing.T, self string, browsers ...string) (system, layout, string) {
	t.Helper()
	home := t.TempDir()
	config := filepath.Join(home, ".config")
	for _, b := range browsers {
		if err := os.MkdirAll(filepath.Join(config, b), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	sys := system{
		getenv: func(k string) string { return map[string]string{"HOME": home}[k] },
		self:   func() (string, error) { return self, nil },
	}
	l, err := currentLayout(sys)
	if err != nil {
		t.Fatal(err)
	}
	return sys, l, config
}

func hostFile(config, browser string) string {
	return filepath.Join(config, browser, "NativeMessagingHosts", hostName+".json")
}

func TestLinuxLayoutFollowsXDG(t *testing.T) {
	home := t.TempDir()
	env := map[string]string{"HOME": home, "XDG_DATA_HOME": filepath.Join(home, "data"), "XDG_CONFIG_HOME": filepath.Join(home, "cfg")}
	l, err := currentLayout(system{getenv: func(k string) string { return env[k] }})
	if err != nil {
		t.Fatal(err)
	}
	if l.exe != filepath.Join(home, "data", "alibi-companion", "alibi-companion") {
		t.Errorf("exe = %s", l.exe)
	}
	if got := l.targets[0].(fileTarget).manifest; got != filepath.Join(home, "cfg", "google-chrome", "NativeMessagingHosts", hostName+".json") {
		t.Errorf("Chrome manifest = %s", got)
	}

	l, _ = currentLayout(system{getenv: func(k string) string { return map[string]string{"HOME": home}[k] }})
	if l.exe != filepath.Join(home, ".local", "share", "alibi-companion", "alibi-companion") {
		t.Errorf("default exe = %s", l.exe)
	}
}

func TestLinuxInstallRegistersWithTheBrowsersThatAreThere(t *testing.T) {
	sys, l, config := linuxSystem(t, fakeDownload(t, "program 1"), "google-chrome", "BraveSoftware/Brave-Browser")
	var out bytes.Buffer
	if err := install(sys, &out, false, true); err != nil {
		t.Fatal(err)
	}
	checkProgram(t, l.exe, "program 1")
	if info, _ := os.Stat(l.exe); info.Mode().Perm() != 0o755 {
		t.Errorf("program mode %v, want 0755", info.Mode().Perm())
	}
	checkManifest(t, hostFile(config, "google-chrome"), l.exe)
	checkManifest(t, hostFile(config, "BraveSoftware/Brave-Browser"), l.exe)
	if _, err := os.Stat(filepath.Join(config, "chromium")); !os.IsNotExist(err) {
		t.Error("created a profile folder for Chromium, which isn't installed")
	}
	if !strings.Contains(out.String(), "Registered with Google Chrome, Brave") {
		t.Errorf("output: %s", out.String())
	}
}

func TestLinuxInstallNeedsABrowser(t *testing.T) {
	sys, l, _ := linuxSystem(t, fakeDownload(t, "program 1"))
	if err := install(sys, &bytes.Buffer{}, false, true); err == nil || !strings.Contains(err.Error(), "no supported browser") {
		t.Errorf("err = %v", err)
	}
	if _, err := os.Stat(l.dir); !os.IsNotExist(err) {
		t.Error("installed without a browser to register with")
	}
}

func TestLinuxReinstallReplacesTheProgram(t *testing.T) {
	self := fakeDownload(t, "program 1")
	sys, l, _ := linuxSystem(t, self, "chromium")
	install(sys, &bytes.Buffer{}, false, true)
	os.WriteFile(self, []byte("program 2"), 0o755)
	if err := install(sys, &bytes.Buffer{}, false, true); err != nil {
		t.Fatal(err)
	}
	checkProgram(t, l.exe, "program 2")
}

func TestLinuxInstallLeavesAnotherProgramAlone(t *testing.T) {
	sys, l, config := linuxSystem(t, fakeDownload(t, "program 1"), "google-chrome", "vivaldi")
	other := hostFile(config, "vivaldi")
	writeFileAtomic(other, hostManifest("/opt/whereabouts/host"), 0o644)
	if err := install(sys, &bytes.Buffer{}, false, true); err == nil || !strings.Contains(err.Error(), "--force") {
		t.Fatalf("install took over another program's registration (err %v)", err)
	}
	if err := install(sys, &bytes.Buffer{}, true, true); err != nil {
		t.Fatal(err)
	}
	checkManifest(t, other, l.exe)
}

func TestLinuxUninstall(t *testing.T) {
	sys, l, config := linuxSystem(t, fakeDownload(t, "program 1"), "google-chrome", "microsoft-edge")
	install(sys, &bytes.Buffer{}, false, true)
	other := hostFile(config, "microsoft-edge")
	writeFileAtomic(other, hostManifest("/opt/other/host"), 0o644) // someone else's since
	var out bytes.Buffer
	if err := uninstall(sys, &out, false); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(hostFile(config, "google-chrome")); !os.IsNotExist(err) {
		t.Error("Chrome's manifest is still there")
	}
	checkManifest(t, other, "/opt/other/host")
	if _, err := os.Stat(l.dir); !os.IsNotExist(err) {
		t.Errorf("%s still there", l.dir)
	}
	if !strings.Contains(out.String(), "Left Microsoft Edge alone") || !strings.Contains(out.String(), "Removed Alibi Companion") {
		t.Errorf("output: %s", out.String())
	}
}

func TestLinuxStatus(t *testing.T) {
	sys, _, _ := linuxSystem(t, fakeDownload(t, "program 1"), "chromium")
	install(sys, &bytes.Buffer{}, false, true)
	var out bytes.Buffer
	status(sys, &out, fakeLookup)
	for _, want := range []string{"Installed: ", "Chromium             registered", "Google Chrome        not installed", "Asia/Tokyo (ja-JP"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("status lacks %q:\n%s", want, out.String())
		}
	}
}
