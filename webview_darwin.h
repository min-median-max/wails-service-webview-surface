// The C face of this service's native layer.
//
// The implementation is in webview_darwin.m. cgo keeps only #cgo directives and this include, so
// the Objective-C has a compilation unit of its own, syntax highlighting, and somewhere to put a
// delegate class — none of which exists inside a Go comment (NATIVE-LAYER N2).
#ifndef SOKSAK_WEBVIEW_DARWIN_H
#define SOKSAK_WEBVIEW_DARWIN_H

#include <stdlib.h>

// One entry of a batch. action is 1 create, 2 update, 3 remove.
typedef struct { int action; void *native; const char *url; const char *surfaceID; int navigate; int interactive; double x,y,width,height; int visible; double alpha; } WebviewOperation;

// What the native layer read back after applying one entry. window is the NSWindow the view is in
// after it was attached, read off the view rather than restated from the argument — a surface in the
// wrong window reads correct on every other number. The frame is CSS top-left, converted
// from the window's bottom-left origin, so both halves of a composition are in one coordinate
// system.
typedef struct { void *native; void *window; double x,y,width,height; double settledX,settledY,settledWidth,settledHeight; int layerContentsRedrawPolicy,layerContentsPlacement; } WebviewResult;

// What the page says about itself, as opposed to what was asked of it. url and title are strdup'd
// and belong to the caller.
typedef struct { const char *url; const char *title; int loading; double progress; } WebviewPageState;

int applyWebviewBatch(void *windowPointer, WebviewOperation *ops, int count, WebviewResult *results, int *resultCount);
int navigateWebview(void *native, const char *rawURL);
int historyWebview(void *native, int delta);
int reloadWebview(void *native);
int stopWebview(void *native);
int webviewPageState(void *native, WebviewPageState *out);

// Starts reporting what a surface's page does, and stops. The reporter is attached at create and
// released at remove, so a surface that is gone sends nothing under a dead id.
//
// The callback is soksakWebviewPageChanged in Go, which cgo cannot declare here — the Go side
// registers itself by including _cgo_export.h.
void watchWebviewPage(void *native, const char *surfaceID);
void unwatchWebviewPage(void *native);

// PNG of what a surface is showing, taken from the view itself.
//
// The window capture composites this process's own layers. A web view draws in another process, so
// its pixels are not in that image and the pane comes back as a flat rectangle. This asks the view.
//
// The buffer is malloc'd and belongs to the caller. Returns 0 on success.
int snapshotWebview(void *native, void **png, int *length);

#endif
