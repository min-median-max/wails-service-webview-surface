//go:build !darwin

package webviewsurface

import (
	"fmt"
	"runtime"
	"unsafe"
)

// This platform has no native web view driver, and every call says so by name.
//
// An empty implementation answering nil would report a navigation as done and leave a blank pane,
// which reads as a broken unit rather than as a platform this build does not cover yet.
//
// The sentence is this service's own words rather than a key in an application's message registry.
// A unit states the fact — which target, which operation — and the application that embeds it words
// that for a person, because the wording is the application's and a unit reaching into one would
// only ever work inside that one.
type appKitWebviewDriver struct{}

func unsupported(operation string) error {
	return fmt.Errorf("webview surface: %s is not implemented on %s in this build — this target has "+
		"no native web view driver", operation, runtime.GOOS)
}

func (appKitWebviewDriver) apply(_ unsafe.Pointer, _ []nativeOperation) ([]nativeResult, error) {
	return nil, unsupported("applying a surface inventory")
}

func (appKitWebviewDriver) navigate(_ unsafe.Pointer, _ string) error {
	return unsupported("navigating")
}

func (appKitWebviewDriver) history(_ unsafe.Pointer, _ int) error {
	return unsupported("stepping the back-forward list")
}

func (appKitWebviewDriver) reload(_ unsafe.Pointer) error {
	return unsupported("reloading")
}

func (appKitWebviewDriver) stop(_ unsafe.Pointer) error {
	return unsupported("stopping a load")
}

func (appKitWebviewDriver) pageState(_ unsafe.Pointer) (pageState, error) {
	return pageState{}, unsupported("reading the page state")
}

func (appKitWebviewDriver) snapshot(_ unsafe.Pointer) ([]byte, error) {
	return nil, unsupported("taking a snapshot")
}
