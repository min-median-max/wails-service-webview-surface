package webviewsurface

import (
	"strings"
	"testing"
	"unsafe"

	compositor "github.com/soksak/wails-service-native-compositor"
)

type recordingDriver struct {
	calls       int
	next        []unsafe.Pointer
	navigations []string
	batches     [][]nativeOperation
}

func (driver *recordingDriver) navigate(_ unsafe.Pointer, url string) error {
	driver.navigations = append(driver.navigations, url)
	return nil
}

func (driver *recordingDriver) history(unsafe.Pointer, int) error { return nil }

func (driver *recordingDriver) reload(unsafe.Pointer) error { return nil }

func (driver *recordingDriver) stop(unsafe.Pointer) error { return nil }

func (driver *recordingDriver) pageState(unsafe.Pointer) (pageState, error) { return pageState{}, nil }

func (driver *recordingDriver) snapshot(unsafe.Pointer) ([]byte, error) { return []byte("png"), nil }

func (driver *recordingDriver) apply(_ unsafe.Pointer, operations []nativeOperation) ([]nativeResult, error) {
	driver.calls++
	driver.batches = append(driver.batches, append([]nativeOperation(nil), operations...))
	results := make([]nativeResult, 0, len(operations))
	for _, operation := range operations {
		native := operation.native
		if operation.action == nativeCreate {
			native = unsafe.Pointer(new(byte))
			driver.next = append(driver.next, native)
		}
		if operation.action != nativeRemove {
			results = append(results, nativeResult{surface: operation.surface, native: native, settledFrame: operation.surface.Frame, layerContentsRedrawPolicy: 2, layerContentsPlacement: 11})
		}
	}
	return results, nil
}

func TestEmptySnapshotRemovesEveryNativeWebviewOwner(t *testing.T) {
	driver := &recordingDriver{}
	backend := newBackend(driver)
	window := byte(1)
	if _, err := backend.Apply(unsafe.Pointer(&window), compositor.Snapshot{Sequence: 1, Surfaces: []compositor.Surface{{
		ID: "webview", Generation: 7, Kind: SurfaceKind,
		Frame: compositor.Frame{Width: 800, Height: 600}, Visible: true, Alpha: 1,
		Source: compositor.SurfaceSource{"url": "https://example.com"},
	}}}); err != nil {
		t.Fatalf("create webview inventory: %v", err)
	}
	if _, err := backend.Apply(unsafe.Pointer(&window), compositor.Snapshot{Sequence: 2}); err != nil {
		t.Fatalf("remove webview inventory: %v", err)
	}
	if len(backend.Status()) != 0 {
		t.Fatalf("empty inventory must leave no webview owners: %+v", backend.Status())
	}
	last := driver.batches[len(driver.batches)-1]
	if len(last) != 1 || last[0].action != nativeRemove || last[0].native == nil {
		t.Fatalf("empty inventory must issue one exact native removal: %+v", last)
	}
}

func TestWebviewBackendOwnsWKWebViewInventoryAndCommands(t *testing.T) {
	driver := &recordingDriver{}
	backend := newBackend(driver)
	window := byte(1)
	snapshot := compositor.Snapshot{Sequence: 1, Surfaces: []compositor.Surface{{
		ID: "webview", Generation: 7, Kind: SurfaceKind,
		Frame: compositor.Frame{Width: 800, Height: 600}, Visible: true, Alpha: 1,
		Source: compositor.SurfaceSource{"url": "https://example.com"},
	}}}
	applied, err := backend.Apply(unsafe.Pointer(&window), snapshot)
	if err != nil || driver.calls != 1 || len(applied) != 1 {
		t.Fatalf("webview inventory must cross one native batch: applied=%+v calls=%d err=%v", applied, driver.calls, err)
	}
	if err := backend.Navigate("webview", 7, "https://wails.io"); err != nil {
		t.Fatalf("navigate current webview owner: %v", err)
	}
	if err := backend.Navigate("webview", 6, "https://stale.invalid"); err == nil {
		t.Fatal("stale webview generation must be rejected")
	}
	status := backend.Status()
	if len(status) != 1 || status[0].ID != "webview" || status[0].Generation != 7 || status[0].URL != "https://wails.io" {
		t.Fatalf("webview status must expose exact current owner and URL: %+v", status)
	}
}

func TestWebviewBackendCarriesInteractiveLayoutPolicyToTheNativeBatch(t *testing.T) {
	driver := &recordingDriver{}
	backend := newBackend(driver)
	window := byte(1)
	snapshot := compositor.Snapshot{Sequence: 1, Interactive: true, Surfaces: []compositor.Surface{{
		ID: "webview", Generation: 7, Kind: SurfaceKind,
		Frame: compositor.Frame{X: 40, Y: 20, Width: 760, Height: 580}, Visible: true, Alpha: 1,
		Source: compositor.SurfaceSource{"url": "https://example.com"},
	}}}
	if _, err := backend.Apply(unsafe.Pointer(&window), snapshot); err != nil {
		t.Fatalf("apply interactive inventory: %v", err)
	}
	if len(driver.batches) != 1 || len(driver.batches[0]) != 1 || !driver.batches[0][0].interactive {
		t.Fatalf("interactive policy must reach the one native batch: %+v", driver.batches)
	}
	status := backend.Status()
	if len(status) != 1 || !status[0].Interactive || status[0].SettledFrame != (compositor.Frame{X: 40, Y: 20, Width: 760, Height: 580}) ||
		status[0].LayerContentsRedrawPolicy != 2 || status[0].LayerContentsPlacement != 11 {
		t.Fatalf("status must expose the phase and raw settled frame: %+v", status)
	}
}

// A host service names the capability it is, never a unit that consumes it.
//
// This assertion held the opposite until 2026-08-20: the name had to be a plugin id. That is the
// defect written down as the rule. Every unit declaring the `webview` permission is served by this
// one code, so naming it after any of them describes the file's old location rather than the
// service, and it puts a plugin id on the host's service list — which is what C1 refuses.
func TestTheServiceNamesTheCapabilityAndNoConsumer(t *testing.T) {
	backend := newBackend(&recordingDriver{})
	service := NewService(backend)
	if name := service.ServiceName(); strings.Contains(name, "soksak-plugin-") {
		t.Fatalf("a host service registered under a unit id puts that id on the host's service list: %q", name)
	}
	if service.ServiceName() != "webview-surface" {
		t.Fatalf("the service names something other than the capability: %q", service.ServiceName())
	}
	if _, err := service.Status(); err != nil {
		t.Fatalf("status command must remain readable before inventory creation: %v", err)
	}
	if err := service.Navigate("missing", 1, "https://example.com"); err == nil {
		t.Fatal("navigate command must return a structured owner error")
	}
}
