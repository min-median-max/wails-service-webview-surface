package webviewsurface

import (
	"os"
	"strings"
	"testing"
)

func TestMakeOwnsWebViewSurfaceCommands(t *testing.T) {
	body, err := os.ReadFile("Makefile")
	if err != nil {
		t.Fatal(err)
	}
	for _, target := range []string{"preflight:", "prepare:", "build:", "verify:"} {
		if !strings.Contains(string(body), target) {
			t.Errorf("Makefile omits %s", target)
		}
	}
	if strings.Contains(string(body), "GO_VERSION :=") {
		t.Error("Makefile duplicates the Go owner")
	}
}
