package webviewsurface

import (
	"encoding/base64"
	"fmt"
	"maps"
	"sort"
	"strings"
	"sync"
	"unsafe"

	compositor "github.com/soksak/wails-service-native-compositor"
)

const SurfaceKind compositor.SurfaceKind = "webview"

type nativeAction uint8

const (
	nativeCreate nativeAction = iota + 1
	nativeUpdate
	nativeRemove
)

type nativeOwner struct {
	generation uint64
	native     unsafe.Pointer
	source     compositor.SurfaceSource
}

type nativeOperation struct {
	action      nativeAction
	surface     compositor.Surface
	native      unsafe.Pointer
	navigate    bool
	interactive bool
}

type nativeResult struct {
	surface compositor.Surface
	native  unsafe.Pointer
	// settledFrame is the raw NSView frame. During an interactive phase surface.Frame is the
	// transformed layer frame a person sees while this one stays at the last WebKit layout.
	settledFrame              compositor.Frame
	layerContentsRedrawPolicy int
	layerContentsPlacement    int
	// window is the window the view is in once it has been attached, read off the view. The
	// compositor compares it with the window it handed over; a driver that leaves it nil is
	// reported misparented rather than believed.
	window unsafe.Pointer
}

type nativeDriver interface {
	apply(window unsafe.Pointer, operations []nativeOperation) ([]nativeResult, error)
	navigate(native unsafe.Pointer, url string) error
	// history moves by whole steps, negative back and positive forward, the way WKWebView counts.
	history(native unsafe.Pointer, delta int) error
	reload(native unsafe.Pointer) error
	stop(native unsafe.Pointer) error
	// pageState is what the page says about itself, as opposed to what was asked of it.
	pageState(native unsafe.Pointer) (pageState, error)
	// snapshot is the surface's own pixels as PNG. The window capture composites this process's
	// layers, and a web view draws in another process, so its pane comes back flat without this.
	snapshot(native unsafe.Pointer) ([]byte, error)
}

// pageState is the page a surface is actually showing.
//
// The declared url is a request. A redirect, a load that failed and a load still running are all
// invisible in it, and on a screen that has not painted yet the three look identical.
type pageState struct {
	URL      string  `json:"url"`
	Title    string  `json:"title"`
	Loading  bool    `json:"loading"`
	Progress float64 `json:"progress"`
}

type SurfaceStatus struct {
	ID                        string           `json:"id"`
	Generation                uint64           `json:"generation"`
	URL                       string           `json:"url"`
	Frame                     compositor.Frame `json:"frame"`
	SettledFrame              compositor.Frame `json:"settledFrame"`
	Interactive               bool             `json:"interactive"`
	LayerContentsRedrawPolicy int              `json:"layerContentsRedrawPolicy"`
	LayerContentsPlacement    int              `json:"layerContentsPlacement"`
	Visible                   bool             `json:"visible"`
	Alpha                     float64          `json:"alpha"`
	Layer                     int              `json:"layer"`
}

type Backend struct {
	mu     sync.Mutex
	driver nativeDriver
	owners map[string]nativeOwner
	status map[string]SurfaceStatus
}

func NewBackend() *Backend { return newBackend(appKitWebviewDriver{}) }

func newBackend(driver nativeDriver) *Backend {
	return &Backend{driver: driver, owners: make(map[string]nativeOwner), status: make(map[string]SurfaceStatus)}
}

func (backend *Backend) Apply(window unsafe.Pointer, snapshot compositor.Snapshot) ([]compositor.AppliedSurface, error) {
	backend.mu.Lock()
	defer backend.mu.Unlock()
	// The handle and the name of one window, together. A pointer report arrives with the handle and
	// the compositor answers by name.
	noteWindow(window, snapshot.Window)
	operations := planNativeBatch(backend.owners, snapshot)
	results, err := backend.driver.apply(window, operations)
	if err != nil {
		return nil, err
	}
	if len(results) != len(snapshot.Surfaces) {
		return nil, fmt.Errorf("webview native batch inventory mismatch: desired=%d applied=%d", len(snapshot.Surfaces), len(results))
	}
	nextOwners := make(map[string]nativeOwner, len(results))
	nextStatus := make(map[string]SurfaceStatus, len(results))
	applied := make([]compositor.AppliedSurface, 0, len(results))
	for _, result := range results {
		if result.native == nil {
			return nil, fmt.Errorf("webview native owner is empty: %s", result.surface.ID)
		}
		surface := result.surface
		nextOwners[surface.ID] = nativeOwner{generation: surface.Generation, native: result.native, source: surface.Source}
		nextStatus[surface.ID] = SurfaceStatus{ID: surface.ID, Generation: surface.Generation, URL: surface.Source["url"], Frame: surface.Frame, SettledFrame: result.settledFrame, Interactive: snapshot.Interactive, LayerContentsRedrawPolicy: result.layerContentsRedrawPolicy, LayerContentsPlacement: result.layerContentsPlacement, Visible: surface.Visible, Alpha: surface.Alpha, Layer: surface.Layer}
		settled := result.settledFrame
		applied = append(applied, compositor.AppliedSurface{ID: surface.ID, Generation: surface.Generation, Frame: surface.Frame, Settled: &settled, LayerContentsRedrawPolicy: result.layerContentsRedrawPolicy, LayerContentsPlacement: result.layerContentsPlacement, Visible: surface.Visible, Alpha: surface.Alpha, Layer: surface.Layer, Window: result.window})
	}
	backend.owners, backend.status = nextOwners, nextStatus
	return applied, nil
}

func (backend *Backend) Navigate(id string, generation uint64, url string) error {
	backend.mu.Lock()
	defer backend.mu.Unlock()
	owner, exists := backend.owners[id]
	if !exists || owner.generation != generation || owner.native == nil {
		return fmt.Errorf("native webview owner does not exist: %s/%d", id, generation)
	}
	if err := backend.driver.navigate(owner.native, url); err != nil {
		return err
	}
	owner.source = maps.Clone(owner.source)
	owner.source["url"] = url
	backend.owners[id] = owner
	status := backend.status[id]
	status.URL = url
	backend.status[id] = status
	return nil
}

// Deliver reads a message the compositor forwarded and drives the surface it names.
//
// The declaration can express the page a pane opens with, because a changed source rebuilds the
// surface. It cannot express going back or reloading: both leave the declared url exactly as it
// was, so a declaration-only webview has four of its five verbs missing.
//
// Every refusal names what was wrong. A caller that receives silence reports a page as moved when
// it never did, and the screen is the only place that disagrees.
func (backend *Backend) Deliver(id string, message map[string]any) (map[string]any, error) {
	verb, named := message["verb"].(string)
	if !named || verb == "" {
		return nil, fmt.Errorf("native webview %s received a message with no verb", id)
	}

	backend.mu.Lock()
	defer backend.mu.Unlock()
	owner, exists := backend.owners[id]
	if !exists || owner.native == nil {
		return nil, fmt.Errorf("native webview %s is not held by this backend", id)
	}

	answer := map[string]any{"id": id, "verb": verb}
	switch verb {
	case "navigate":
		raw, given := message["url"].(string)
		if !given || strings.TrimSpace(raw) == "" {
			// Loading an empty string blanks the page, and a blank page reads on screen exactly
			// like a crash.
			return nil, fmt.Errorf("native webview %s was asked to navigate with no url", id)
		}
		url := normalizeURL(raw)
		if err := backend.driver.navigate(owner.native, url); err != nil {
			return nil, err
		}
		// The record follows the page. Leaving it on the declared url makes the next inventory
		// commit see a changed source and rebuild the surface back to where it started.
		owner.source = maps.Clone(owner.source)
		owner.source["url"] = url
		backend.owners[id] = owner
		status := backend.status[id]
		status.URL = url
		backend.status[id] = status
		answer["url"] = url
	case "history":
		// The step comes as a number, not a direction. The back-forward list takes whole steps, so
		// a jump of three is one call, and a verb that only said "back" would turn it into one.
		delta, ok := numberOf(message["delta"])
		if !ok || delta == 0 {
			return nil, fmt.Errorf("native webview %s was asked to step history by %v, which is neither back nor forward", id, message["delta"])
		}
		if err := backend.driver.history(owner.native, delta); err != nil {
			return nil, err
		}
		answer["delta"] = delta
	case "reload":
		if err := backend.driver.reload(owner.native); err != nil {
			return nil, err
		}
	case "stop":
		if err := backend.driver.stop(owner.native); err != nil {
			return nil, err
		}
	case "snapshot":
		png, err := backend.driver.snapshot(owner.native)
		if err != nil {
			return nil, err
		}
		// Base64 because the answer crosses a JSON boundary. A path would mean a temporary file per
		// capture, and a caller that never reads it leaves the file behind.
		answer["png"] = base64.StdEncoding.EncodeToString(png)
		answer["bytes"] = len(png)
	case "state":
		state, err := backend.driver.pageState(owner.native)
		if err != nil {
			return nil, err
		}
		answer["url"] = state.URL
		answer["title"] = state.Title
		answer["loading"] = state.Loading
		answer["progress"] = state.Progress
		// What was asked for, next to what happened. Equal is the ordinary case; different is a
		// redirect or a load that never landed, and a caller with only one of them cannot tell.
		answer["declared"] = owner.source["url"]
	default:
		return nil, fmt.Errorf("native webview %s answers no verb named %s", id, verb)
	}
	return answer, nil
}

// numberOf reads a step that crossed the wire. JSON has one number type, so it arrives as a
// float64 here and as an int in a Go caller's map; both are the same step.
func numberOf(value any) (int, bool) {
	switch typed := value.(type) {
	case int:
		return typed, true
	case float64:
		if typed != float64(int(typed)) {
			return 0, false
		}
		return int(typed), true
	default:
		return 0, false
	}
}

// normalizeURL is the same rule the page half of a unit driving this applies, so a bare host typed into either
// one opens the same address.
func normalizeURL(value string) string {
	trimmed := strings.TrimSpace(value)
	lowered := strings.ToLower(trimmed)
	if strings.HasPrefix(lowered, "http://") || strings.HasPrefix(lowered, "https://") {
		return trimmed
	}
	return "https://" + trimmed
}

func (backend *Backend) Status() []SurfaceStatus {
	backend.mu.Lock()
	defer backend.mu.Unlock()
	result := make([]SurfaceStatus, 0, len(backend.status))
	for _, status := range backend.status {
		result = append(result, status)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].ID < result[j].ID })
	return result
}

func planNativeBatch(current map[string]nativeOwner, snapshot compositor.Snapshot) []nativeOperation {
	desired := append([]compositor.Surface(nil), snapshot.Surfaces...)
	sort.Slice(desired, func(i, j int) bool {
		if desired[i].Layer != desired[j].Layer {
			return desired[i].Layer < desired[j].Layer
		}
		return desired[i].ID < desired[j].ID
	})
	seen := make(map[string]struct{}, len(desired))
	operations := make([]nativeOperation, 0, len(current)+len(desired))
	for _, surface := range desired {
		seen[surface.ID] = struct{}{}
		owner, exists := current[surface.ID]
		if !exists {
			operations = append(operations, nativeOperation{action: nativeCreate, surface: surface, interactive: snapshot.Interactive})
		} else if owner.generation != surface.Generation {
			operations = append(operations,
				nativeOperation{action: nativeRemove, surface: compositor.Surface{ID: surface.ID, Generation: owner.generation}, native: owner.native},
				nativeOperation{action: nativeCreate, surface: surface, interactive: snapshot.Interactive},
			)
		} else {
			operations = append(operations, nativeOperation{action: nativeUpdate, surface: surface, native: owner.native, navigate: !maps.Equal(owner.source, surface.Source), interactive: snapshot.Interactive})
		}
	}
	removed := make([]string, 0)
	for id := range current {
		if _, exists := seen[id]; !exists {
			removed = append(removed, id)
		}
	}
	sort.Strings(removed)
	for _, id := range removed {
		owner := current[id]
		operations = append(operations, nativeOperation{action: nativeRemove, surface: compositor.Surface{ID: id, Generation: owner.generation}, native: owner.native})
	}
	return operations
}

type Service struct{ backend *Backend }

func NewService(backend *Backend) *Service { return &Service{backend: backend} }

func (service *Service) ServiceName() string { return "webview-surface" }

func (service *Service) Navigate(id string, generation uint64, url string) error {
	if service.backend == nil {
		return fmt.Errorf("native webview backend is not configured")
	}
	return service.backend.Navigate(id, generation, url)
}

func (service *Service) Status() ([]SurfaceStatus, error) {
	if service.backend == nil {
		return nil, fmt.Errorf("native webview backend is not configured")
	}
	return service.backend.Status(), nil
}
