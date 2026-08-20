package webviewsurface

import (
	"testing"
	"unsafe"

	"github.com/soksak/soksak-core/core/contentview"
)

// Where the pointer landed, reported as the fact the core already names.
//
// A page receives its own clicks and the document above it never sees them, so a click inside a
// webview left the focused pane where it was — measured on the running build 2026-08-17.
//
// This side holds what the compositor cannot know: that a window handle and a window name are the
// same window. Which surface is under the point is the compositor's, and asking it is what keeps
// the rectangles in one place — an earlier attempt walked the native view tree here and landed
// short by the title bar's height.
func TestThePointerIsReportedAsTheSurfaceUnderIt(t *testing.T) {
	handle := unsafe.Pointer(new(byte))

	var emitted []struct {
		name    string
		payload any
	}
	PublishPagesTo(func(name string, payload any) {
		emitted = append(emitted, struct {
			name    string
			payload any
		}{name, payload})
	})
	t.Cleanup(func() { PublishPagesTo(nil) })

	var asked []string
	ReadSurfacesWith(func(window string, x float64, y float64) string {
		asked = append(asked, window)
		if x > 100 {
			return "webview.win-a.tab-b"
		}
		return ""
	})
	t.Cleanup(func() { ReadSurfacesWith(nil) })

	// Nothing has said which window this handle is yet.
	publishPointerDown(handle, 200, 50)
	if len(emitted) != 0 {
		t.Fatalf("a handle no apply has named was reported anyway: %+v", emitted)
	}

	noteWindow(handle, "win-a")

	// A point on no surface is a point on the document, which sees its own clicks.
	publishPointerDown(handle, 10, 50)
	if len(emitted) != 0 {
		t.Fatalf("a point on no surface was reported: %+v", emitted)
	}

	publishPointerDown(handle, 200, 50)
	if len(emitted) != 1 {
		t.Fatalf("a point on a surface was reported %d times", len(emitted))
	}
	if emitted[0].name != contentview.Activated {
		t.Errorf("the pointer travelled as %q, and the core acts on %q", emitted[0].name, contentview.Activated)
	}
	payload, ok := emitted[0].payload.(map[string]any)
	if !ok || payload["label"] != "webview.win-a.tab-b" {
		t.Errorf("the report named %+v, and the surface under the point is webview.win-a.tab-b", emitted[0].payload)
	}
	if len(asked) == 0 || asked[len(asked)-1] != "win-a" {
		t.Errorf("the compositor was asked about %v, and the handle was applied against win-a", asked)
	}
}
