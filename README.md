# wails-service-webview-surface

The host's child web view, as a native surface.

One `WKWebView` per declared surface: created, moved, navigated, stepped through its back-forward
list, reloaded, stopped, captured, and reported. It implements `Backend` for
`wails-service-native-compositor`, which owns the inventory, the sequencing and the receipts and
knows nothing about what kind of surface it is placing.

## What this is not

It is not a browser. Tabs, an address bar, bookmarks, history as a list, downloads — none of that is
here, and none of it should be. Every verb in this service is a `WKWebView` method, and every unit
that declares the `webview` permission is served by this one code.

That is also why it is here rather than in a unit. Until 2026-08-20 these 1,290 lines sat in one
browser unit's repository and its service answered to that unit's id, so a unit id was on the host's
service list at run time. Three browser units written against this substrate declare the `webview`
permission; the code was one of them's by where the file sat and by nothing else.

## Shape

- `backend.go` — the inventory half: which surface a declaration owns, what changed, what the
  driver was told, what came back.
- `native_darwin.go`, `webview_darwin.h`, `webview_darwin.m` — the driver. The Objective-C has a
  compilation unit of its own; cgo keeps only `#cgo` directives and the include (NATIVE-LAYER N2).
- `native_unsupported.go` — every other target fails by name rather than leaving a blank pane.
- `pagereport.go` — url, title, loading and progress, as the view reports them.

## Verification

```sh
make verify
```

`go.mod` is the exact Go owner. This repository verifies its public Backend implementation and
native driver boundary. The compositor verifies inventory sequencing, while the product repository
verifies the real multi-service window composition.

On Darwin, a surface picture captures the complete `WKWebView` bounds and includes pending
WebContent updates. The picture is therefore the current native pixel replacement used while the
host temporarily hides a live surface for document capture; returning an earlier blank frame is a
driver contract failure.

Every geometry commit, including each interactive divider preview, applies one complete viewport:
the host rectangle, `WKWebView` bounds, dim veil and reported settled frame change together. Moving
only a clipping host is forbidden because it leaves page layout at the previous width until release.
