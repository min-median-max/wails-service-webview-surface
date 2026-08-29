package webviewsurface

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

// The interactive fact changes what is done, and the clip does not depend on history.
//
// A snapshot carries `interactive` from the layout system's begin edge to its matching end edge.
// It crosses the document's observer, the compositor's Go half, this backend and the C boundary,
// and the observer keeps a queue of the edges so a begin and an end are never coalesced away. All
// of that exists to reach one branch.
//
// Reviewed 2026-08-20: the two arms of that branch did the same thing. `presentWebviewInteractively`
// and `settleWebviewFrame` both set the host's frame, fit the web view to the host's bounds and
// record the settled rect — the same three statements in the same order. Whatever a drag gained
// came from the host view, its redraw policy and the transaction around them, none of which asks
// whether the phase is interactive. A fact carried through four layers to a branch with one
// behaviour is a fact nothing depends on.
//
// The one difference was `masksToBounds`, and it was the wrong difference: set on the interactive
// arm and nowhere else, a surface that is never dragged never has its host layer clipped. Whether
// the web view can draw outside its host is a property of the box, not of whether a person has ever
// dragged it — and a box changes without a drag every time a window is resized, a pane is split, or
// a tab is maximised.
//
// Read from the source. The behaviour is in Objective-C on the main thread of a running window, and
// what is asserted here is that the two paths are not the same text and that the clip is established
// where the host is.
func TestInteractivePresentationIsADifference(t *testing.T) {
	body, err := os.ReadFile("webview_darwin.m")
	if err != nil {
		t.Fatalf("reading the driver: %v", err)
	}
	source := string(body)

	interactive := cFunctionBody(t, source, "static BOOL presentWebviewInteractively(WKWebView *view, NSRect wanted) {")
	settled := cFunctionBody(t, source, "static BOOL settleWebviewFrame(WKWebView *view, NSRect wanted) {")

	if statements(interactive) == statements(settled) {
		t.Error("presentWebviewInteractively and settleWebviewFrame do the same thing.\n" +
			"`interactive` crosses the observer, the compositor, this backend and the C boundary to\n" +
			"reach this branch. Give the interactive arm behaviour of its own, or take the fact out\n" +
			"of all four layers.")
	}

	// The clip belongs to the host, established once where the host is made. Left to the interactive
	// arm, it arrives only for a surface that has been dragged.
	create := cFunctionBody(t, source, "int applyWebviewBatch(void *windowPointer, WebviewOperation *ops, int count, WebviewResult *results, int *resultCount) {")
	if !strings.Contains(create, "masksToBounds") {
		t.Error("the host's layer is not clipped where the host is created.\n" +
			"Whether the web view can draw outside its host is a property of the box. Set it beside\n" +
			"wantsLayer and the redraw policy, so it holds for a surface no one has dragged.")
	}
}

// Dimming a native surface must produce the same final luminance as the document lighting plane.
// The webview is above that plane, so fading its host blends the page with an already-dimmed DOM
// placeholder and makes the two media disagree. The host owns an opaque black backing and only the
// page content fades against it.
func TestNativeDimUsesOneBlackBackingInsteadOfTheDimmedDocument(t *testing.T) {
	body, err := os.ReadFile("webview_darwin.m")
	if err != nil {
		t.Fatalf("reading the driver: %v", err)
	}
	source := string(body)
	create := cFunctionBody(t, source, "int applyWebviewBatch(void *windowPointer, WebviewOperation *ops, int count, WebviewResult *results, int *resultCount) {")
	if !strings.Contains(create, "NSView *backing = [[NSView alloc] initWithFrame:host.bounds]") ||
		!strings.Contains(create, "backing.layer.backgroundColor = NSColor.blackColor.CGColor") ||
		!strings.Contains(create, "[host addSubview:backing]") {
		t.Error("the native surface host has no explicit black sibling behind the remote webview layer")
	}
	if strings.Contains(create, "host.alphaValue = op.alpha") {
		t.Error("host alpha blends the page with the already-dimmed document")
	}
	if !strings.Contains(create, "view.alphaValue = op.alpha") {
		t.Error("the page content does not receive the declared native alpha")
	}
}

// statements is the body with comments, whitespace and line breaks removed — what the code does,
// with nothing that only says why.
func statements(body string) string {
	withoutBlock := regexp.MustCompile(`(?s)/\*.*?\*/`).ReplaceAllString(body, "")
	withoutLine := regexp.MustCompile(`(?m)//.*$`).ReplaceAllString(withoutBlock, "")
	return strings.Join(strings.Fields(withoutLine), " ")
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
