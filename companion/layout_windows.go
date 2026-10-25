package main

import (
	"errors"
	"path/filepath"

	winreg "golang.org/x/sys/windows/registry"
)

// On Windows, a browser finds a native host through a registry key under HKCU whose default value
// is the path of the host's manifest. Keys for browsers that aren't installed are harmless, and
// they mean a browser installed later finds the companion too.
var windowsBrowsers = []struct{ name, key string }{
	{"Google Chrome", `Google\Chrome`}, // some other Chromium browsers read Chrome's key as well
	{"Microsoft Edge", `Microsoft\Edge`},
	{"Brave", `BraveSoftware\Brave-Browser`},
	{"Chromium", `Chromium`},
}

func currentLayout(sys system) (layout, error) {
	base := sys.getenv("LOCALAPPDATA")
	if base == "" {
		return layout{}, errors.New("LOCALAPPDATA isn't set, so there's no user folder to install into")
	}
	dir := filepath.Join(base, "Programs", "Alibi Companion")
	l := layout{dir: dir, exe: filepath.Join(dir, "alibi-companion.exe"), manifest: filepath.Join(dir, hostName+".json"), agent: newAgentFiles(dir)}
	l.owner = l.manifest
	if appData := sys.getenv("APPDATA"); appData != "" {
		l.agent.shortcut = filepath.Join(appData, "Microsoft", "Windows", "Start Menu", "Programs", "Alibi Companion.lnk")
	}
	for _, b := range windowsBrowsers {
		l.targets = append(l.targets, registryTarget{
			name:     b.name,
			key:      `Software\` + b.key + `\NativeMessagingHosts\` + hostName,
			manifest: l.manifest,
			registry: sys.registry,
		})
	}
	return l, nil
}

type registryTarget struct {
	name, key, manifest string
	registry            registry
}

func (t registryTarget) browser() string { return t.name }
func (t registryTarget) found() bool     { return true }

func (t registryTarget) owner() (string, error) {
	value, found, err := t.registry.value(t.key, "")
	if !found {
		return "", err
	}
	return value, err
}

func (t registryTarget) register() error   { return t.registry.setValue(t.key, "", t.manifest) }
func (t registryTarget) unregister() error { return t.registry.deleteKey(t.key) }

func userRegistry() registry { return hkcu{} }

// hkcu is the current user's part of the real registry.
type hkcu struct{}

func (hkcu) value(key, name string) (string, bool, error) {
	k, err := winreg.OpenKey(winreg.CURRENT_USER, key, winreg.QUERY_VALUE)
	if errors.Is(err, winreg.ErrNotExist) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	defer k.Close()
	value, _, err := k.GetStringValue(name)
	if errors.Is(err, winreg.ErrNotExist) {
		return "", false, nil
	}
	return value, err == nil, err
}

func (hkcu) setValue(key, name, value string) error {
	k, _, err := winreg.CreateKey(winreg.CURRENT_USER, key, winreg.SET_VALUE)
	if err != nil {
		return err
	}
	defer k.Close()
	return k.SetStringValue(name, value)
}

func (hkcu) setNumber(key, name string, value uint32) error {
	k, _, err := winreg.CreateKey(winreg.CURRENT_USER, key, winreg.SET_VALUE)
	if err != nil {
		return err
	}
	defer k.Close()
	return k.SetDWordValue(name, value)
}

func (hkcu) deleteValue(key, name string) error {
	k, err := winreg.OpenKey(winreg.CURRENT_USER, key, winreg.SET_VALUE)
	if errors.Is(err, winreg.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	defer k.Close()
	if err := k.DeleteValue(name); err != nil && !errors.Is(err, winreg.ErrNotExist) {
		return err
	}
	return nil
}

func (hkcu) deleteKey(key string) error {
	err := winreg.DeleteKey(winreg.CURRENT_USER, key)
	if errors.Is(err, winreg.ErrNotExist) {
		return nil
	}
	return err
}
