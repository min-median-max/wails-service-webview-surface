package webviewsurface

import (
	"os"
	"strings"
	"testing"
)

func TestDarwinSnapshotIncludesPendingWebContentUpdates(t *testing.T) {
	source, err := os.ReadFile("webview_darwin.m")
	if err != nil {
		t.Fatal(err)
	}
	text := string(source)
	if !strings.Contains(text, "config.rect = CGRectNull") {
		t.Fatal("Darwin webview snapshot does not select the complete bounds")
	}
	if !strings.Contains(text, "config.afterScreenUpdates = YES") {
		t.Fatal("Darwin webview snapshot can return before pending WebContent updates")
	}
}
