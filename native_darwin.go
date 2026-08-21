//go:build darwin

package webviewsurface

/*
#cgo CFLAGS: -x objective-c -fblocks -Wno-deprecated-declarations
#cgo LDFLAGS: -framework Cocoa -framework QuartzCore -framework WebKit
#include "webview_darwin.h"
*/
import "C"

import (
	"fmt"
	"unsafe"

	compositor "github.com/soksak-ai/wails-service-native-compositor"
)

type appKitWebviewDriver struct{}

func (appKitWebviewDriver) apply(window unsafe.Pointer, operations []nativeOperation) ([]nativeResult, error) {
	if len(operations) == 0 {
		return []nativeResult{}, nil
	}
	cOps := make([]C.WebviewOperation, len(operations))
	urls := make([]*C.char, len(operations))
	ids := make([]*C.char, len(operations))
	defer func() {
		for i := range urls {
			if urls[i] != nil {
				C.free(unsafe.Pointer(urls[i]))
			}
			if ids[i] != nil {
				C.free(unsafe.Pointer(ids[i]))
			}
		}
	}()
	for i, operation := range operations {
		if operation.action != nativeRemove {
			urls[i] = C.CString(operation.surface.Source["url"])
			// The id travels with the create so the reporter knows what to report under. Looking
			// it up later would tie a report to whichever surface was found first.
			ids[i] = C.CString(operation.surface.ID)
		}
		cOps[i] = C.WebviewOperation{
			action: C.int(operation.action), native: operation.native, url: urls[i], surfaceID: ids[i], navigate: C.int(asInt(operation.navigate)), interactive: C.int(asInt(operation.interactive)),
			x: C.double(operation.surface.Frame.X), y: C.double(operation.surface.Frame.Y), width: C.double(operation.surface.Frame.Width), height: C.double(operation.surface.Frame.Height),
			visible: C.int(asInt(operation.surface.Visible)), alpha: C.double(operation.surface.Alpha),
		}
	}
	cResults := make([]C.WebviewResult, len(operations))
	var count C.int
	if status := C.applyWebviewBatch(window, &cOps[0], C.int(len(cOps)), &cResults[0], &count); status != 0 {
		return nil, fmt.Errorf("apply WKWebView batch: status=%d", int(status))
	}
	results := make([]nativeResult, 0, int(count))
	output := 0
	for _, operation := range operations {
		if operation.action == nativeRemove {
			continue
		}
		result := cResults[output]
		output++
		surface := operation.surface
		surface.Frame = compositorFrame(result)
		results = append(results, nativeResult{surface: surface, native: result.native, settledFrame: settledCompositorFrame(result), layerContentsRedrawPolicy: int(result.layerContentsRedrawPolicy), layerContentsPlacement: int(result.layerContentsPlacement), window: unsafe.Pointer(result.window)})
	}
	return results, nil
}

func (appKitWebviewDriver) navigate(native unsafe.Pointer, url string) error {
	raw := C.CString(url)
	defer C.free(unsafe.Pointer(raw))
	if status := C.navigateWebview(native, raw); status != 0 {
		return fmt.Errorf("navigate WKWebView: status=%d", int(status))
	}
	return nil
}

func compositorFrame(result C.WebviewResult) compositor.Frame {
	return compositor.Frame{X: float64(result.x), Y: float64(result.y), Width: float64(result.width), Height: float64(result.height)}
}

func settledCompositorFrame(result C.WebviewResult) compositor.Frame {
	return compositor.Frame{X: float64(result.settledX), Y: float64(result.settledY), Width: float64(result.settledWidth), Height: float64(result.settledHeight)}
}

func asInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

func (appKitWebviewDriver) pageState(native unsafe.Pointer) (pageState, error) {
	var out C.WebviewPageState
	if status := C.webviewPageState(native, &out); status != 0 {
		return pageState{}, fmt.Errorf("read WKWebView state: status=%d", int(status))
	}
	state := pageState{Loading: out.loading != 0, Progress: float64(out.progress)}
	if out.url != nil {
		state.URL = C.GoString(out.url)
		C.free(unsafe.Pointer(out.url))
	}
	if out.title != nil {
		state.Title = C.GoString(out.title)
		C.free(unsafe.Pointer(out.title))
	}
	return state, nil
}

func (appKitWebviewDriver) snapshot(native unsafe.Pointer) ([]byte, error) {
	var buffer unsafe.Pointer
	var length C.int
	if status := C.snapshotWebview(native, &buffer, &length); status != 0 {
		return nil, fmt.Errorf("snapshot WKWebView: status=%d", int(status))
	}
	defer C.free(buffer)
	return C.GoBytes(buffer, length), nil
}

func (appKitWebviewDriver) history(native unsafe.Pointer, delta int) error {
	if status := C.historyWebview(native, C.int(delta)); status != 0 {
		return fmt.Errorf("step WKWebView history: status=%d", int(status))
	}
	return nil
}

func (appKitWebviewDriver) reload(native unsafe.Pointer) error {
	if status := C.reloadWebview(native); status != 0 {
		return fmt.Errorf("reload WKWebView: status=%d", int(status))
	}
	return nil
}

func (appKitWebviewDriver) stop(native unsafe.Pointer) error {
	if status := C.stopWebview(native); status != 0 {
		return fmt.Errorf("stop WKWebView: status=%d", int(status))
	}
	return nil
}
