//go:build darwin

package webviewsurface

/*
#include <stdlib.h>
*/
import "C"

import "unsafe"

// soksakWebviewPointerDown is what the native monitor calls when the pointer lands in a window.
//
// Its own file for the same reason as the page report: cgo forbids a definition in the preamble of a
// file that exports a symbol.
//
// The point is in the coordinate contract every surface is declared in, and the window arrives as
// the handle it was applied against. Which surface is under the point is not decided here: the
// compositor holds the rectangles.
//
//export soksakWebviewPointerDown
func soksakWebviewPointerDown(window unsafe.Pointer, x C.double, y C.double) {
	publishPointerDown(window, float64(x), float64(y))
}
