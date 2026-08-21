package webviewsurface

import (
	"sync"
	"unsafe"

	contentview "github.com/soksak-ai/soksak-contract-contentview"
)

// pageReport is one moment of what a surface's page is doing.
//
// The declared url is what someone asked for and it does not move when a link is followed, a
// redirect lands, or a load fails. A person watching a webview sees all three, so the page reports
// them as they happen instead of leaving a caller to poll.
//
// canBack and canForward travel with the rest because they are facts of the same moment. Read
// separately afterwards they answer about a later one, and the symptom is a back button that is
// enabled a frame too early or disabled a frame too late.
type pageReport struct {
	ID string `json:"id"`
	// Changed is which property moved, as the view names it: URL, title, loading or
	// estimatedProgress. The whole state travels with every report, but a consumer that wanted one
	// event per change would otherwise have to diff, and a diff of a title against a title it
	// never saw reports a change that did not happen.
	Changed    string  `json:"changed"`
	URL        string  `json:"url"`
	Title      string  `json:"title"`
	Loading    bool    `json:"loading"`
	Progress   float64 `json:"progress"`
	CanBack    bool    `json:"canBack"`
	CanForward bool    `json:"canForward"`
}

// pageSink receives one report. The host supplies the emitter behind it; this package does not
// know how an event leaves the process.
type pageSink func(report pageReport)

var reporter struct {
	mu   sync.RWMutex
	sink pageSink
}

// PublishPagesTo names where a page's events leave the process.
//
// Nil silences them, which is what a host with no event bus wants — a report with nowhere to go is
// dropped here rather than panicking inside a native callback, where the stack ends in Objective-C
// and says nothing about which surface was involved.
//
// The host passes an emitter and nothing else. Which content view event a moved property becomes
// is decided below, in the package that keeps the history — a core that made that decision would
// have to know that a second step backwards exists, and a back button is not a fact about panes.
func PublishPagesTo(emit func(name string, payload any)) {
	reporter.mu.Lock()
	defer reporter.mu.Unlock()
	if emit == nil {
		reporter.sink = nil
		pointer.mu.Lock()
		pointer.emit = nil
		pointer.mu.Unlock()
		return
	}
	reporter.sink = pageEvents(emit)

	// The pointer travels the same way and is named by the core, so it takes the same emitter.
	pointer.mu.Lock()
	defer pointer.mu.Unlock()
	pointer.emit = emit
}

// pageEvents turns one report into the single event that moved.
//
// Fanning all three out on every change would emit a navigation each time a title arrived, and a
// consumer that resets the title on navigation would erase the title it had just been given.
func pageEvents(emit func(name string, payload any)) pageSink {
	return func(report pageReport) {
		switch report.Changed {
		case "URL":
			// inPage is false: this comes from the view's URL property, which a page-side bridge
			// could distinguish and this one cannot. Claiming true would tell a consumer the
			// document did not change when it may have.
			emit(contentview.Navigated, map[string]any{
				"label": report.ID, "url": report.URL, "inPage": false,
			})
		case "title":
			emit(contentview.Title, map[string]any{"label": report.ID, "title": report.Title})
		case "loading", "estimatedProgress":
			emit(contentview.Loading, map[string]any{
				"label": report.ID, "loading": report.Loading,
				"canBack": report.CanBack, "canForward": report.CanForward,
			})
		}
		// Anything else is dropped. Emitting under a name nobody listens for is invisible work that
		// reads, from outside, exactly like an event that was never sent.
	}
}

// publishPointerDown reports where the pointer landed.
//
// A page receives its own clicks and the document above it never sees them, so a click inside a
// webview left the focused pane where it was — measured 2026-08-17. The core already names this fact
// and already acts on it; nothing was saying it happened.
//
// Which surface is under the point is the compositor's answer: it holds every applied rectangle in
// the contract they are declared in. This side holds only what the compositor cannot know — that a
// window handle and a window name are the same window, which every apply states.
//
// It travels under the core's own name rather than an event of one unit's: which pane and
// which tab focus means is the core's, and this states only that the person clicked here.
func publishPointerDown(window unsafe.Pointer, x float64, y float64) {
	pointer.mu.RLock()
	emit := pointer.emit
	at := pointer.surfaceAt
	name := pointer.windows[window]
	pointer.mu.RUnlock()
	if emit == nil || at == nil || name == "" {
		return
	}
	surfaceID := at(name, x, y)
	if surfaceID == "" {
		// The point is on the document, which sees its own clicks. Nothing to report.
		return
	}
	emit(contentview.Activated, map[string]any{"label": surfaceID})
}

// ReadSurfacesWith names who answers which surface a point is on.
//
// The compositor holds the rectangles and this package holds the window handles, so neither can
// answer alone. Nil silences the pointer, which is what a host with no compositor wants.
func ReadSurfacesWith(surfaceAt func(window string, x float64, y float64) string) {
	pointer.mu.Lock()
	defer pointer.mu.Unlock()
	pointer.surfaceAt = surfaceAt
}

// noteWindow records that a handle and a name are the same window. Every apply states both.
func noteWindow(handle unsafe.Pointer, name string) {
	if handle == nil || name == "" {
		return
	}
	pointer.mu.Lock()
	defer pointer.mu.Unlock()
	if pointer.windows == nil {
		pointer.windows = make(map[unsafe.Pointer]string, 2)
	}
	pointer.windows[handle] = name
}

var pointer struct {
	mu        sync.RWMutex
	emit      func(name string, payload any)
	surfaceAt func(window string, x float64, y float64) string
	windows   map[unsafe.Pointer]string
}

// publishPageReport hands one report to whoever is listening.
func publishPageReport(report pageReport) {
	reporter.mu.RLock()
	sink := reporter.sink
	reporter.mu.RUnlock()
	// Every consumer filters by label, and the label is the surface id. A report without one is
	// delivered to everyone or to no one depending on how each filter was written.
	if sink == nil || report.ID == "" {
		return
	}
	sink(report)
}
