package webviewsurface

import (
	"errors"
	"strings"
	"testing"
	"unsafe"

	compositor "github.com/soksak-ai/wails-service-native-compositor"
)

// The five verbs a person expects of a webview, and what the surface answers about itself.
//
// The declaration places the surface and rebuilds it, so it can express the page a pane opens with
// — but not going back, and not reloading, because both leave the declared url unchanged. Those
// arrive as messages the compositor forwards without reading, and this is where they are read.

type historyDriver struct {
	recordingDriver
	steps   []int
	reloads int
	stops   int
	refuse  error
}

func (driver *historyDriver) history(_ unsafe.Pointer, delta int) error {
	driver.steps = append(driver.steps, delta)
	return driver.refuse
}

func (driver *historyDriver) reload(_ unsafe.Pointer) error {
	driver.reloads++
	return driver.refuse
}

func (driver *historyDriver) stop(_ unsafe.Pointer) error {
	driver.stops++
	return driver.refuse
}

// A backend holding one surface, the way one exists after a commit.
func oneSurface(t *testing.T, driver nativeDriver) *Backend {
	t.Helper()
	backend := newBackend(driver)
	// A real allocation, not a made-up address. checkptr rejects pointer arithmetic on an
	// invented one, and the failure lands as a fatal error with no test name attached.
	window := unsafe.Pointer(new(byte))
	_, err := backend.Apply(window, compositor.Snapshot{
		Sequence: 1,
		Surfaces: []compositor.Surface{{
			ID: "brw-a", Kind: "webview", Generation: 1, Alpha: 1, Visible: true,
			Frame:  compositor.Frame{X: 0, Y: 0, Width: 100, Height: 100},
			Source: map[string]string{"url": "https://example.com"},
		}},
	})
	if err != nil {
		t.Fatalf("applying the fixture inventory: %v", err)
	}
	return backend
}

func TestEachVerbReachesTheNativeLayer(t *testing.T) {
	for _, probe := range []struct {
		verb  string
		extra map[string]any
		check func(*historyDriver) bool
		what  string
	}{
		{"navigate", map[string]any{"url": "example.org"}, func(d *historyDriver) bool {
			return len(d.navigations) == 1 && d.navigations[0] == "https://example.org"
		}, "one navigation to the normalized url"},
		{"history", map[string]any{"delta": -1}, func(d *historyDriver) bool { return len(d.steps) == 1 && d.steps[0] == -1 }, "one step back"},
		{"history", map[string]any{"delta": 1.0}, func(d *historyDriver) bool { return len(d.steps) == 1 && d.steps[0] == 1 }, "one step forward from a wire number"},
		{"history", map[string]any{"delta": -3}, func(d *historyDriver) bool { return len(d.steps) == 1 && d.steps[0] == -3 }, "one jump of three"},
		{"reload", nil, func(d *historyDriver) bool { return d.reloads == 1 }, "one reload"},
		{"stop", nil, func(d *historyDriver) bool { return d.stops == 1 }, "one stop"},
	} {
		driver := &historyDriver{}
		backend := oneSurface(t, driver)
		message := map[string]any{"verb": probe.verb}
		for key, value := range probe.extra {
			message[key] = value
		}
		if _, err := backend.Deliver("brw-a", message); err != nil {
			t.Fatalf("%s: %v", probe.verb, err)
		}
		if !probe.check(driver) {
			t.Errorf("%s did not produce %s", probe.verb, probe.what)
		}
	}
}

func TestAVerbThisKindDoesNotAnswerIsRefusedByName(t *testing.T) {
	// Silence here is a caller that believes a surface did something it never did. The refusal
	// names the verb, because "invalid message" sends the reader to the wrong file.
	backend := oneSurface(t, &historyDriver{})
	_, err := backend.Deliver("brw-a", map[string]any{"verb": "seek"})
	if err == nil {
		t.Fatal("an unknown verb was accepted")
	}
	if !strings.Contains(err.Error(), "seek") {
		t.Errorf("the refusal reads %q and does not name the verb", err)
	}
}

func TestAHistoryStepOfZeroIsRefused(t *testing.T) {
	// Zero is neither back nor forward. Passing it to the back-forward list asks for the item the
	// page is already on, which does nothing and reads as a frozen button.
	driver := &historyDriver{}
	backend := oneSurface(t, driver)
	if _, err := backend.Deliver("brw-a", map[string]any{"verb": "history", "delta": 0}); err == nil {
		t.Fatal("a step of zero was accepted")
	}
	if len(driver.steps) != 0 {
		t.Errorf("the native layer was asked anyway: %v", driver.steps)
	}
}

func TestAMessageWithNoVerbIsRefused(t *testing.T) {
	backend := oneSurface(t, &historyDriver{})
	if _, err := backend.Deliver("brw-a", map[string]any{"url": "example.org"}); err == nil {
		t.Fatal("a message with no verb was accepted")
	}
}

func TestNavigateWithNoURLIsRefusedRatherThanLoadingNothing(t *testing.T) {
	// Loading an empty string blanks the page, which reads on screen exactly like a crash.
	driver := &historyDriver{}
	backend := oneSurface(t, driver)
	if _, err := backend.Deliver("brw-a", map[string]any{"verb": "navigate"}); err == nil {
		t.Fatal("navigate with no url was accepted")
	}
	if len(driver.navigations) != 0 {
		t.Errorf("the native layer was asked anyway: %v", driver.navigations)
	}
}

func TestASurfaceThisBackendDoesNotHoldIsRefused(t *testing.T) {
	backend := oneSurface(t, &historyDriver{})
	if _, err := backend.Deliver("brw-ghost", map[string]any{"verb": "reload"}); err == nil {
		t.Fatal("a message for an unheld surface was accepted")
	}
}

func TestTheNativeLayersRefusalComesBack(t *testing.T) {
	// The verb is known and the native call failed. Answering ok would leave the caller reading a
	// success for a page that never moved.
	driver := &historyDriver{refuse: errNativeRefused}
	backend := oneSurface(t, driver)
	if _, err := backend.Deliver("brw-a", map[string]any{"verb": "reload"}); err == nil {
		t.Fatal("the native refusal was swallowed")
	}
}

func TestTheAnswerCarriesTheSurfaceAndTheVerb(t *testing.T) {
	// The caller is a command that has to report what it did. A bare ok gives it nothing to say.
	backend := oneSurface(t, &historyDriver{})
	answer, err := backend.Deliver("brw-a", map[string]any{"verb": "navigate", "url": "example.org"})
	if err != nil {
		t.Fatalf("delivering: %v", err)
	}
	if answer["id"] != "brw-a" || answer["verb"] != "navigate" || answer["url"] != "https://example.org" {
		t.Errorf("the answer is %v", answer)
	}
}

// errNativeRefused stands for whatever the native layer reports when a call does not land.
var errNativeRefused = errors.New("WKWebView refused")
