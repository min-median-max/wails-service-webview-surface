package webviewsurface

import (
	"reflect"
	"sync"
	"testing"

	contentview "github.com/soksak-ai/soksak-contract-contentview"
)

type recordedEvent struct {
	name    string
	payload map[string]any
}

func recordingEmitter() (*[]recordedEvent, func(string, any)) {
	var seen []recordedEvent
	return &seen, func(name string, payload any) {
		seen = append(seen, recordedEvent{name: name, payload: payload.(map[string]any)})
	}
}

// One report becomes the single event that moved.
//
// The native layer reports the whole state on every change, because reading a second property
// afterwards answers about a later moment. The content view vocabulary is narrower: a navigation, a
// title, and a load starting or stopping. The split happens in this package, against what actually
// moved — fanning all three out on every change would emit a navigation every time a title
// arrived, and a consumer that resets the title on navigation would erase it.
func TestAPageReportBecomesTheEventThatMoved(t *testing.T) {
	for _, probe := range []struct {
		changed string
		name    string
		payload map[string]any
	}{
		{"URL", contentview.Navigated, map[string]any{"label": "brw-a", "url": "https://example.com/", "inPage": false}},
		{"title", contentview.Title, map[string]any{"label": "brw-a", "title": "Example Domain"}},
		{"loading", contentview.Loading, map[string]any{"label": "brw-a", "loading": true, "canBack": true, "canForward": false}},
		{"estimatedProgress", contentview.Loading, map[string]any{"label": "brw-a", "loading": true, "canBack": true, "canForward": false}},
	} {
		seen, emit := recordingEmitter()
		PublishPagesTo(emit)
		publishPageReport(pageReport{
			ID: "brw-a", Changed: probe.changed,
			URL: "https://example.com/", Title: "Example Domain",
			Loading: true, Progress: 0.4, CanBack: true, CanForward: false,
		})
		PublishPagesTo(nil)
		if len(*seen) != 1 {
			t.Fatalf("%s produced %d events, not 1: %v", probe.changed, len(*seen), *seen)
		}
		if (*seen)[0].name != probe.name {
			t.Errorf("%s produced %q, not %q", probe.changed, (*seen)[0].name, probe.name)
		}
		if !reflect.DeepEqual((*seen)[0].payload, probe.payload) {
			t.Errorf("%s carried %v, not %v", probe.changed, (*seen)[0].payload, probe.payload)
		}
	}
}

func TestAReportOfSomethingWithNoEventIsDropped(t *testing.T) {
	// Emitting under a name nobody listens for is invisible work that reads, from the outside,
	// exactly like an event that was never sent.
	seen, emit := recordingEmitter()
	PublishPagesTo(emit)
	defer PublishPagesTo(nil)
	publishPageReport(pageReport{ID: "brw-a", Changed: "canGoBack"})
	if len(*seen) != 0 {
		t.Errorf("an unmapped change produced %v", *seen)
	}
}

func TestAReportWithNoListenerIsDropped(t *testing.T) {
	PublishPagesTo(nil)
	// The measurement is that this returns. A nil sink called from a native callback takes the
	// process down with a stack that says nothing about which surface was involved.
	publishPageReport(pageReport{ID: "brw-a", Changed: "URL", URL: "https://example.com/"})
}

func TestAReportWithNoSurfaceIsDropped(t *testing.T) {
	// An empty id belongs to no pane, and the label every consumer filters on is that id.
	// Publishing it puts an event on the bus that every subscriber filters out, which is invisible
	// work, and one that filters by presence would take it.
	seen, emit := recordingEmitter()
	PublishPagesTo(emit)
	defer PublishPagesTo(nil)
	publishPageReport(pageReport{Changed: "URL", URL: "https://example.com/"})
	if len(*seen) != 0 {
		t.Errorf("a report with no surface produced %v", *seen)
	}
}

func TestTheListenerCanBeReplacedWhileReportsArrive(t *testing.T) {
	// Reports come from the main thread and the host sets the sink during boot. A read racing a
	// write is a data race the race detector catches, and in production a torn read of a func
	// value is a jump to nowhere.
	var wg sync.WaitGroup
	stop := make(chan struct{})
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
				publishPageReport(pageReport{ID: "brw-a", Changed: "title"})
			}
		}
	}()
	for i := 0; i < 200; i++ {
		PublishPagesTo(func(string, any) {})
	}
	close(stop)
	wg.Wait()
	PublishPagesTo(nil)
}
