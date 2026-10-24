package main

import (
	"bytes"
	"errors"
	"testing"
)

func TestMacOSLeavesTheHostToWhereabouts(t *testing.T) {
	if err := install(defaultSystem(), &bytes.Buffer{}, false, true); !errors.Is(err, errMacOS) {
		t.Errorf("install on macOS: %v, want errMacOS", err)
	}
}
