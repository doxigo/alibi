package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// A stand-in for the downloaded program that install copies into place.
func fakeDownload(t *testing.T, contents string) string {
	t.Helper()
	self := filepath.Join(t.TempDir(), "alibi-companion-download")
	if err := os.WriteFile(self, []byte(contents), 0o755); err != nil {
		t.Fatal(err)
	}
	return self
}

func readManifest(t *testing.T, path string) map[string]any {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("no manifest: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("manifest isn't JSON: %v", err)
	}
	return m
}

func checkManifest(t *testing.T, path, exe string) {
	t.Helper()
	m := readManifest(t, path)
	if m["name"] != hostName || m["type"] != "stdio" || !samePath(m["path"].(string), exe) {
		t.Errorf("manifest %s = %v, want %s starting %s", path, m, hostName, exe)
	}
	origins, _ := m["allowed_origins"].([]any)
	if len(origins) != 1 || origins[0] != extensionOrigin {
		t.Errorf("allowed_origins = %v, want only Alibi", origins)
	}
}

func checkProgram(t *testing.T, exe, want string) {
	t.Helper()
	b, err := os.ReadFile(exe)
	if err != nil {
		t.Fatalf("program not installed: %v", err)
	}
	if string(b) != want {
		t.Errorf("installed program = %q, want %q", b, want)
	}
	old, _ := filepath.Glob(filepath.Join(filepath.Dir(exe), "*.old"))
	if len(old) > 0 {
		t.Errorf("left set-aside copies behind: %v", old)
	}
}

func fakeLookup(context.Context) (cover, error) { return tokyo, nil }

func TestHostManifest(t *testing.T) {
	var m map[string]any
	if err := json.Unmarshal(hostManifest("/opt/alibi-companion"), &m); err != nil {
		t.Fatal(err)
	}
	if m["path"] != "/opt/alibi-companion" || m["name"] != hostName {
		t.Errorf("got %v", m)
	}
}
