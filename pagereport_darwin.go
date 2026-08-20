//go:build darwin

package webviewsurface

/*
#include <stdlib.h>
*/
import "C"

import "unsafe"

// soksakWebviewPageChanged is what the native reporter calls when a page moves.
//
// It is in its own file because cgo forbids a definition in the preamble of a file that exports a
// symbol, and webview_darwin.go carries the #cgo directives and the header include.
//
// The strings belong to the caller and are gone when this returns, so they are copied here.
//
//export soksakWebviewPageChanged
func soksakWebviewPageChanged(
	surfaceID *C.char,
	changed *C.char,
	url *C.char,
	title *C.char,
	loading C.int,
	progress C.double,
	canBack C.int,
	canForward C.int,
) {
	publishPageReport(pageReport{
		ID:         C.GoString(surfaceID),
		Changed:    C.GoString(changed),
		URL:        C.GoString(url),
		Title:      C.GoString(title),
		Loading:    loading != 0,
		Progress:   float64(progress),
		CanBack:    canBack != 0,
		CanForward: canForward != 0,
	})
}

var _ = unsafe.Pointer(nil)
