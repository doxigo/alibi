//go:build !windows && !darwin

package main

import (
	"errors"
	"os"
	"path/filepath"
)

// On Linux, a browser finds a native host through a manifest file in a NativeMessagingHosts folder
// inside its profile folder. Browsers installed as a Snap or Flatpak can't start programs outside
// their sandbox, so they don't use these, and Alibi keeps asking GeoJS itself.
var linuxBrowsers = []struct{ name, dir string }{
	{"Google Chrome", "google-chrome"},
	{"Google Chrome Beta", "google-chrome-beta"},
	{"Google Chrome Dev", "google-chrome-unstable"},
	{"Chromium", "chromium"},
	{"Brave", "BraveSoftware/Brave-Browser"},
	{"Microsoft Edge", "microsoft-edge"},
	{"Vivaldi", "vivaldi"},
}

func currentLayout(sys system) (layout, error) {
	home := sys.getenv("HOME")
	data, config := sys.getenv("XDG_DATA_HOME"), sys.getenv("XDG_CONFIG_HOME")
	if home == "" && (data == "" || config == "") {
		return layout{}, errors.New("HOME isn't set, so there's no user folder to install into")
	}
	if data == "" {
		data = filepath.Join(home, ".local", "share")
	}
	if config == "" {
		config = filepath.Join(home, ".config")
	}
	dir := filepath.Join(data, "alibi-companion")
	l := layout{dir: dir, exe: filepath.Join(dir, "alibi-companion")}
	l.owner = l.exe
	for _, b := range linuxBrowsers {
		profile := filepath.Join(config, b.dir)
		l.targets = append(l.targets, fileTarget{
			name:     b.name,
			profile:  profile,
			manifest: filepath.Join(profile, "NativeMessagingHosts", hostName+".json"),
			exe:      l.exe,
		})
	}
	return l, nil
}

type fileTarget struct {
	name, profile, manifest, exe string
}

func (t fileTarget) browser() string { return t.name }

func (t fileTarget) found() bool {
	info, err := os.Stat(t.profile)
	return err == nil && info.IsDir()
}

func (t fileTarget) owner() (string, error) {
	if _, err := os.Stat(t.manifest); errors.Is(err, os.ErrNotExist) {
		return "", nil
	}
	program, err := programIn(t.manifest)
	if err != nil {
		// Unreadable: someone else's, as far as install and uninstall are concerned.
		return t.manifest, nil
	}
	return program, nil
}

func (t fileTarget) register() error { return writeFileAtomic(t.manifest, hostManifest(t.exe), 0o644) }

func (t fileTarget) unregister() error {
	if err := os.Remove(t.manifest); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

func userRegistry() registry { return nil }
