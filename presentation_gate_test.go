package webviewsurface

import (
	"os"
	"strings"
	"testing"
)

// Every interactive preview applies the complete viewport rectangle.
//
// Moving only the clipping host leaves WKWebView at its previous width until mouse-up. The panel
// boundary then follows the pointer while the visible page viewport remains narrow. `Applied.Frame`
// can still equal the declaration because it reports the host, so `Settled` must also advance on
// every preview. One AppKit transaction sets the host, the WKWebView bounds, the dim veil and the
// reported viewport before the next paint.
func TestInteractivePresentationAppliesTheCompleteViewport(t *testing.T) {
	body, err := os.ReadFile("webview_darwin.m")
	if err != nil {
		t.Fatalf("reading the driver: %v", err)
	}
	source := string(body)

	placement := cFunctionBody(t, source, "static BOOL placeWebviewFrame(WKWebView *view, NSRect wanted) {")
	for _, statement := range []string{
		"host.frame = wanted",
		"view.frame = host.bounds",
		"host.dimOverlay.frame = wanted",
		"host.settledFrame = wanted",
	} {
		if !strings.Contains(placement, statement) {
			t.Errorf("interactive preview does not apply %q", statement)
		}
	}
	apply := cFunctionBody(t, source, "int applyWebviewBatch(void *windowPointer, WebviewOperation *ops, int count, WebviewResult *results, int *resultCount) {")
	if !strings.Contains(apply, "placeWebviewFrame(view, wanted)") {
		t.Error("the native batch does not apply the complete viewport placement")
	}
	if strings.Contains(apply, "op.interactive") {
		t.Error("interactive preview still selects a host-only geometry branch")
	}

	// The clip belongs to the host and is established once where the host is made. A gesture-only
	// placement branch would leave a surface that has never been dragged without this box rule.
	if !strings.Contains(apply, "masksToBounds") {
		t.Error("the host's layer is not clipped where the host is created.\n" +
			"Whether the web view can draw outside its host is a property of the box. Set it beside\n" +
			"wantsLayer and the redraw policy, so it holds for a surface no one has dragged.")
	}
}

// Dimming a native surface must produce the same final luminance as the document lighting plane.
// The webview is above that plane, so fading it blends the page with an already-dimmed DOM
// placeholder and makes the two media disagree. A pointer-transparent black veil above the page
// applies the same operation as the document lighting plane.
func TestNativeDimUsesOneVeilInsteadOfTheDimmedDocument(t *testing.T) {
	body, err := os.ReadFile("webview_darwin.m")
	if err != nil {
		t.Fatalf("reading the driver: %v", err)
	}
	source := string(body)
	create := cFunctionBody(t, source, "int applyWebviewBatch(void *windowPointer, WebviewOperation *ops, int count, WebviewResult *results, int *resultCount) {")
	if !strings.Contains(source, "@interface SoksakDimOverlay : NSView") ||
		!strings.Contains(source, "- (NSView *)hitTest:(NSPoint)point { return nil; }") {
		t.Error("the native dim veil is not an explicit pointer-transparent view")
	}
	if strings.Contains(create, "host.alphaValue = op.alpha") {
		t.Error("host alpha blends the page with the already-dimmed document")
	}
	if strings.Contains(create, "view.alphaValue = op.alpha") {
		t.Error("webview alpha blends the remote page layer with the already-dimmed document")
	}
	if !strings.Contains(create, "dimOverlay.alphaValue = 1.0 - op.alpha") ||
		!strings.Contains(create, "[content addSubview:dimOverlay]") ||
		!strings.Contains(create, "[wanted addObject:dimOverlay]") {
		t.Error("the declared dim is not painted once above the native page")
	}
}

// cFunctionBody is the text between a function's opening line and the closing brace in column one.
func cFunctionBody(t *testing.T, source string, signature string) string {
	t.Helper()
	at := strings.Index(source, signature)
	if at < 0 {
		t.Fatalf("the driver has no %s — this gate names a function that moved", signature)
	}
	rest := source[at+len(signature):]
	end := strings.Index(rest, "\n}")
	if end < 0 {
		t.Fatalf("%s has no closing brace in column one", signature)
	}
	return rest[:end]
}
