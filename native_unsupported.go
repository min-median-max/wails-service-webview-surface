//go:build !darwin

package webviewsurface

import (
	"unsafe"

	"github.com/soksak/soksak-core/core/i18n"
)

// This platform has no native webview driver, and it says so by name.
//
// An empty implementation that answered nil would report a navigation as done
// and leave a blank pane, which reads as a broken unit rather than a platform
// this build does not cover yet. The sentence reaches a person — someone opened
// a webview tab — so it comes from a key.
type appKitWebviewDriver struct{}

func init() {
	i18n.Declare(map[string]i18n.Sentence{
		"webview.native.unsupported": {
			EN: "this platform has no native webview in this build",
			KO: "이 빌드에는 이 플랫폼용 네이티브 브라우저가 없습니다",
		},
	})
}

func (appKitWebviewDriver) apply(_ unsafe.Pointer, _ []nativeOperation) ([]nativeResult, error) {
	return nil, i18n.Errorf("webview.native.unsupported", nil)
}

func (appKitWebviewDriver) navigate(_ unsafe.Pointer, _ string) error {
	return i18n.Errorf("webview.native.unsupported", nil)
}

func (appKitWebviewDriver) history(_ unsafe.Pointer, _ int) error {
	return i18n.Errorf("webview.native.unsupported", nil)
}

func (appKitWebviewDriver) reload(_ unsafe.Pointer) error {
	return i18n.Errorf("webview.native.unsupported", nil)
}

func (appKitWebviewDriver) stop(_ unsafe.Pointer) error {
	return i18n.Errorf("webview.native.unsupported", nil)
}

func (appKitWebviewDriver) pageState(_ unsafe.Pointer) (pageState, error) {
	return pageState{}, i18n.Errorf("webview.native.unsupported", nil)
}

func (appKitWebviewDriver) snapshot(_ unsafe.Pointer) ([]byte, error) {
	return nil, i18n.Errorf("webview.native.unsupported", nil)
}
