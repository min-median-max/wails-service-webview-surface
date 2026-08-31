// The native layer: WKWebViews created, placed and driven on the main thread.
//
// Every entry point hops to the main queue when it is not already there. AppKit and WebKit are
// main-thread only, and a call from a Go goroutine is on whatever thread the scheduler chose.
#import <Cocoa/Cocoa.h>
#import <QuartzCore/QuartzCore.h>
#import <WebKit/WebKit.h>

#include "webview_darwin.h"
#include "_cgo_export.h"

// What the page does, reported as it happens.
//
// A person watching a webview sees the address change, the title arrive, and a spinner start and
// stop. None of that is in the declaration — the declared url is what was asked for, and it does
// not move when a link is followed. So the view is observed and every change is one report.
//
// One reporter per surface, holding the id it reports under. Keeping a shared reporter and looking
// the id up would tie a report to whichever surface happened to be found first.
@interface SoksakWebviewReporter : NSObject <WKNavigationDelegate>
@property (copy) NSString *surfaceID;
@end

@implementation SoksakWebviewReporter

// The four the page moves through. estimatedProgress is included because a load that stalls looks
// exactly like a load that finished when only loading is reported.
+ (NSArray<NSString *> *)observed {
    return @[@"URL", @"title", @"loading", @"estimatedProgress"];
}

- (void)observeValueForKeyPath:(NSString *)keyPath
                      ofObject:(id)object
                        change:(NSDictionary *)change
                       context:(void *)context {
    WKWebView *view = (WKWebView *)object;
    NSString *url = view.URL.absoluteString;
    NSString *title = view.title;
    // The Go side copies before returning, so these autoreleased buffers are enough.
    soksakWebviewPageChanged(
        (char *)self.surfaceID.UTF8String,
        (char *)keyPath.UTF8String,
        (char *)(url == nil ? "" : url.UTF8String),
        (char *)(title == nil ? "" : title.UTF8String),
        view.isLoading ? 1 : 0,
        view.estimatedProgress,
        view.canGoBack ? 1 : 0,
        view.canGoForward ? 1 : 0);
}

@end

// A native lighting veil is visual only. Returning nil lets AppKit continue hit testing to the
// WKWebView below it, so matching the document lighting plane never changes input ownership.
@interface SoksakDimOverlay : NSView
@end

@implementation SoksakDimOverlay
- (NSView *)hitTest:(NSPoint)point { return nil; }
@end

// One AppKit-owned presentation layer around one WebKit-owned view.
//
// The host is the sole geometry owner and the WKWebView fills its local bounds. Both carry
// DuringViewResize/TopLeft layer policy before any frame is applied, matching AppKit's live-resize
// contract without a second transform or clipping geometry path.
@interface SoksakWebviewHost : NSView
@property (assign) WKWebView *webview;
@property (assign) SoksakDimOverlay *dimOverlay;
@property NSRect settledFrame;
@end

@implementation SoksakWebviewHost
@end

static SoksakWebviewHost *webviewHost(WKWebView *view) {
    NSView *host = view.superview;
    if (![host isKindOfClass:[SoksakWebviewHost class]]) return nil;
    return (SoksakWebviewHost *)host;
}

// The reporters, keyed by the view they watch. A view holds no strong reference to its observer,
// so something has to, and releasing it at remove is what stops a report under a dead id.
static NSMapTable *webviewReporters(void) {
    static NSMapTable *table = nil;
    static dispatch_once_t once;
    dispatch_once(&once, ^{
        table = [[NSMapTable alloc] initWithKeyOptions:NSPointerFunctionsOpaquePersonality
                                          valueOptions:NSPointerFunctionsStrongMemory
                                              capacity:8];
    });
    return table;
}

// The pointer, reported as a point.
//
// A page receives its own clicks and the document above it never sees them, so a click inside a
// webview left the focused pane where it was — measured on the running build 2026-08-17.
//
// A point and the window it landed in, and nothing more. Which surface is under it is answered by
// the compositor, which holds every surface's applied rectangle in the coordinate contract they are
// declared in; deciding it here means re-deriving that in whatever coordinate space this file
// happens to be in, and the first attempt did exactly that and landed short by the title bar.
//
// The same flip `webviewRect` applies when placing, inverted: the contract is CSS top-left and
// AppKit measures from the bottom.
//
// A local monitor rather than a subclass or a delegate: the event is read on its way past and
// returned unchanged, so the page's own handling of the click is untouched.
static id webviewPointerMonitor = nil;

static void startWebviewPointerMonitor(void) {
    if (webviewPointerMonitor != nil) return;
    webviewPointerMonitor = [[NSEvent addLocalMonitorForEventsMatchingMask:NSEventMaskLeftMouseDown
                                                                   handler:^NSEvent *(NSEvent *event) {
        NSWindow *window = event.window;
        NSView *content = window.contentView;
        if (window != nil && content != nil) {
            NSPoint at = event.locationInWindow;
            // The window by its native handle. Its name is the document's, and the Go side already
            // holds both together — every apply arrives with the handle and the label.
            soksakWebviewPointerDown((void *)window, at.x, NSHeight(content.bounds) - at.y);
        }
        // Returned unchanged: this reads the click, it does not take it.
        return event;
    }] retain];
}

void watchWebviewPage(void *native, const char *surfaceID) {
    if (native == NULL || surfaceID == NULL) return;
    dispatch_block_t block = ^{
        WKWebView *view = (WKWebView *)native;
        if ([webviewReporters() objectForKey:(__bridge id)native] != nil) return;
        SoksakWebviewReporter *reporter = [[SoksakWebviewReporter alloc] init];
        reporter.surfaceID = [NSString stringWithUTF8String:surfaceID];
        for (NSString *path in [SoksakWebviewReporter observed]) {
            [view addObserver:reporter forKeyPath:path options:NSKeyValueObservingOptionNew context:NULL];
        }
        [webviewReporters() setObject:reporter forKey:(__bridge id)native];
        [reporter release];
        // One monitor for every surface this service owns, started with the first of them.
        startWebviewPointerMonitor();
    };
    if ([NSThread isMainThread]) block(); else dispatch_sync(dispatch_get_main_queue(), block);
}

void unwatchWebviewPage(void *native) {
    if (native == NULL) return;
    dispatch_block_t block = ^{
        SoksakWebviewReporter *reporter = [webviewReporters() objectForKey:(__bridge id)native];
        if (reporter == nil) return;
        WKWebView *view = (WKWebView *)native;
        for (NSString *path in [SoksakWebviewReporter observed]) {
            [view removeObserver:reporter forKeyPath:path];
        }
        [webviewReporters() removeObjectForKey:(__bridge id)native];
    };
    if ([NSThread isMainThread]) block(); else dispatch_sync(dispatch_get_main_queue(), block);
}


static NSRect webviewRect(NSView *content, WebviewOperation op) {
    return NSMakeRect(op.x, NSHeight(content.bounds) - op.y - op.height, op.width, op.height);
}

// One document geometry commit is one complete native viewport commit. Moving only the clipping
// host leaves WKWebView at its previous width until mouse-up, so the border follows the pointer
// while the page stays narrow. Apply host, content and veil in this one disabled-action transaction.
static BOOL placeWebviewFrame(WKWebView *view, NSRect wanted) {
    SoksakWebviewHost *host = webviewHost(view);
    if (host == nil) return NO;
    if (!NSEqualRects(host.frame, wanted)) host.frame = wanted;
    if (!NSEqualRects(view.frame, host.bounds)) view.frame = host.bounds;
    if (!NSEqualRects(host.dimOverlay.frame, wanted)) host.dimOverlay.frame = wanted;
    host.settledFrame = wanted;
    return YES;
}

// The host is the box actually presented; settledFrame is where WebKit last laid out its page.
static NSRect presentedWebviewFrame(WKWebView *view) {
    SoksakWebviewHost *host = webviewHost(view);
    if (host == nil) return NSZeroRect;
    return host.frame;
}

int applyWebviewBatch(void *windowPointer, WebviewOperation *ops, int count, WebviewResult *results, int *resultCount) {
    if (windowPointer == NULL) return 1;
    __block int status = 0;
    dispatch_block_t block = ^{
        NSView *content = ((NSWindow *)windowPointer).contentView;
        if (content == nil) { status = 2; return; }
        [CATransaction begin];
        [CATransaction setDisableActions:YES];
        for (int i = 0; i < count; i++) {
            if (ops[i].action != 1) continue;
            WKWebViewConfiguration *config = [[WKWebViewConfiguration alloc] init];
            SoksakWebviewHost *host = [[SoksakWebviewHost alloc] initWithFrame:webviewRect(content, ops[i])];
            host.wantsLayer = YES;
            // Whether the web view can draw outside its host is a property of the box, so it is
            // settled where the box is made. Set on the interactive path alone until 2026-08-20, a
            // surface nobody had dragged was never clipped — and a box changes without a drag every
            // time a window is resized, a pane is split or a tab is maximised.
            host.layer.masksToBounds = YES;
            host.autoresizingMask = NSViewNotSizable;
            host.layerContentsRedrawPolicy = NSViewLayerContentsRedrawDuringViewResize;
            host.layerContentsPlacement = NSViewLayerContentsPlacementTopLeft;
            WKWebView *view = [[WKWebView alloc] initWithFrame:host.bounds configuration:config];
            [config release];
            if (host == nil || view == nil) {
                if (view != nil) [view release];
                if (host != nil) [host release];
                status = 3;
                break;
            }
            view.autoresizingMask = NSViewNotSizable;
            view.layerContentsRedrawPolicy = NSViewLayerContentsRedrawDuringViewResize;
            view.layerContentsPlacement = NSViewLayerContentsPlacementTopLeft;
            SoksakDimOverlay *dimOverlay = [[SoksakDimOverlay alloc] initWithFrame:host.frame];
            dimOverlay.wantsLayer = YES;
            dimOverlay.layer.backgroundColor = NSColor.blackColor.CGColor;
            dimOverlay.alphaValue = 0;
            dimOverlay.autoresizingMask = NSViewNotSizable;
            host.webview = view;
            host.dimOverlay = dimOverlay;
            host.settledFrame = host.frame;
            [host addSubview:view];
            [content addSubview:dimOverlay];
            [dimOverlay release];
            ops[i].native = view;
        }
        if (status != 0) {
            for (int i = 0; i < count; i++) {
                if (ops[i].action != 1 || ops[i].native == NULL) continue;
                WKWebView *view = (WKWebView *)ops[i].native;
                SoksakWebviewHost *host = webviewHost(view);
                [host.dimOverlay removeFromSuperview];
                [view removeFromSuperview];
                [view release];
                [host release];
            }
            [CATransaction commit];
            return;
        }
        for (int i = 0; i < count; i++) {
            WebviewOperation op = ops[i];
            if (op.action == 3) {
                WKWebView *view = (WKWebView *)op.native;
                SoksakWebviewHost *host = webviewHost(view);
                if (host == nil) { status = 4; break; }
                unwatchWebviewPage(view);
                [view stopLoading];
                [host.dimOverlay removeFromSuperview];
                [view removeFromSuperview];
                [view release];
                [host removeFromSuperview];
                [host release];
                continue;
            }
            WKWebView *view = (WKWebView *)op.native;
            SoksakWebviewHost *host = webviewHost(view);
            if (host == nil) { status = 4; break; }
            // Written only when it differs.
            //
            // A layout moves by committing a rectangle per frame, and every assignment here marks
            // the view's layer for a commit whether or not the value changed. The same lesson as
            // the reorder below, one field further in: measured 2026-08-17, four of six focus
            // moves stopped the window drawing for 56 to 59ms and every one of them was a move
            // this page took part in.
            if (view.autoresizingMask != NSViewNotSizable) view.autoresizingMask = NSViewNotSizable;
            NSRect wanted = webviewRect(content, op);
            BOOL placed = placeWebviewFrame(view, wanted);
            if (!placed) { status = 4; break; }
            BOOL hide = op.visible == 0;
            if (host.hidden != hide) host.hidden = hide;
            SoksakDimOverlay *dimOverlay = host.dimOverlay;
            if (dimOverlay == nil) { status = 4; break; }
            if (dimOverlay.alphaValue != 1.0 - op.alpha) dimOverlay.alphaValue = 1.0 - op.alpha;
            BOOL hideDim = hide || op.alpha >= 1.0;
            if (dimOverlay.hidden != hideDim) dimOverlay.hidden = hideDim;
            if (op.action == 1 && op.surfaceID != NULL) watchWebviewPage(view, op.surfaceID);
            if ((op.action == 1 || op.navigate != 0) && op.url != NULL) {
                NSURL *url = [NSURL URLWithString:[NSString stringWithUTF8String:op.url]];
                if (url != nil) [view loadRequest:[NSURLRequest requestWithURL:url]];
            }
        }
        if (status != 0) {
            [CATransaction commit];
            return;
        }
        // Attach only what is not attached, and reorder only when the order is wrong.
        //
        // -addSubview:positioned: on a view that is already a subview removes it and puts it back:
        // the layer is detached and reattached, on the main thread, for every surface in every
        // commit. A layout moves by committing a rectangle per frame, so this ran the whole
        // hierarchy through a teardown per frame — measured 2026-08-17, the window stopped drawing
        // for 68 to 217ms on a focus change while the paths in the document accounted for under 20
        // of it.
        //
        // The declared order is the order the operations arrive in, topmost last. It is compared
        // against what the window already holds, and nothing is touched when they agree.
        NSMutableArray<NSView *> *wanted = [NSMutableArray arrayWithCapacity:count * 2];
        for (int i = 0; i < count; i++) {
            if (ops[i].action == 3) continue;
            SoksakWebviewHost *host = webviewHost((WKWebView *)ops[i].native);
            if (host == nil) { status = 4; break; }
            [wanted addObject:host];
            SoksakDimOverlay *dimOverlay = host.dimOverlay;
            if (dimOverlay == nil) { status = 4; break; }
            [wanted addObject:dimOverlay];
        }
        if (status != 0) { [CATransaction commit]; return; }
        BOOL ordered = YES;
        NSUInteger at = 0;
        for (NSView *subview in content.subviews) {
            if (![wanted containsObject:subview]) continue;
            if (at >= wanted.count || wanted[at] != subview) { ordered = NO; break; }
            at++;
        }
        if (at != wanted.count) ordered = NO;
        for (NSView *view in wanted) {
            if (view.superview != content) { ordered = NO; break; }
        }
        if (!ordered) {
            for (NSView *view in wanted) {
                [content addSubview:view positioned:NSWindowAbove relativeTo:nil];
            }
        }

        [CATransaction commit];

        int output = 0;
        for (int i = 0; i < count; i++) {
            WebviewOperation op = ops[i];
            if (op.action == 3) continue;
            WKWebView *view = (WKWebView *)op.native;
            SoksakWebviewHost *host = webviewHost(view);
            if (host == nil) { status = 4; break; }
            NSRect frame = presentedWebviewFrame(view);
            NSRect settled = host.settledFrame;
            results[output++] = (WebviewResult){
                view, host.window,
                frame.origin.x, NSHeight(content.bounds) - NSMaxY(frame), frame.size.width, frame.size.height,
                settled.origin.x, NSHeight(content.bounds) - NSMaxY(settled), settled.size.width, settled.size.height,
                (int)host.layerContentsRedrawPolicy, (int)host.layerContentsPlacement
            };
        }
        if (status != 0) return;
        *resultCount = output;
    };
    if ([NSThread isMainThread]) block(); else dispatch_sync(dispatch_get_main_queue(), block);
    return status;
}

int navigateWebview(void *native, const char *rawURL) {
    if (native == NULL || rawURL == NULL) return 1;
    __block int status = 0;
    dispatch_block_t block = ^{
        NSURL *url = [NSURL URLWithString:[NSString stringWithUTF8String:rawURL]];
        if (url == nil) { status = 2; return; }
        [(WKWebView *)native loadRequest:[NSURLRequest requestWithURL:url]];
    };
    if ([NSThread isMainThread]) block(); else dispatch_sync(dispatch_get_main_queue(), block);
    return status;
}

// Whole steps through the back-forward list, negative back and positive forward. WKWebView answers
// nil when the step does not exist, and that is not an error: a person at the first page pressing
// back is asking for something reasonable that has no effect.
int historyWebview(void *native, int delta) {
    if (native == NULL || delta == 0) return 1;
    __block int status = 0;
    dispatch_block_t block = ^{
        WKWebView *view = (WKWebView *)native;
        WKBackForwardListItem *item = [view.backForwardList itemAtIndex:delta];
        if (item != nil) [view goToBackForwardListItem:item];
    };
    if ([NSThread isMainThread]) block(); else dispatch_sync(dispatch_get_main_queue(), block);
    return status;
}

int reloadWebview(void *native) {
    if (native == NULL) return 1;
    dispatch_block_t block = ^{ [(WKWebView *)native reload]; };
    if ([NSThread isMainThread]) block(); else dispatch_sync(dispatch_get_main_queue(), block);
    return 0;
}

// What the page says about itself, read from the view rather than from what was asked for.
//
// The declared url is what someone wanted; these are what happened. A redirect, a failed load and a
// page still loading are all invisible in the declaration, and all three look the same on a screen
// that has not painted yet.

int webviewPageState(void *native, WebviewPageState *out) {
    if (native == NULL || out == NULL) return 1;
    __block int status = 0;
    dispatch_block_t block = ^{
        WKWebView *view = (WKWebView *)native;
        NSString *url = view.URL.absoluteString;
        NSString *title = view.title;
        // strdup because the block's autoreleased strings do not survive the return into Go.
        out->url = url == nil ? NULL : strdup(url.UTF8String);
        out->title = title == nil ? NULL : strdup(title.UTF8String);
        out->loading = view.isLoading ? 1 : 0;
        out->progress = view.estimatedProgress;
    };
    if ([NSThread isMainThread]) block(); else dispatch_sync(dispatch_get_main_queue(), block);
    return status;
}

int stopWebview(void *native) {
    if (native == NULL) return 1;
    dispatch_block_t block = ^{ [(WKWebView *)native stopLoading]; };
    if ([NSThread isMainThread]) block(); else dispatch_sync(dispatch_get_main_queue(), block);
    return 0;
}
int snapshotWebview(void *native, void **png, int *length) {
    if (native == NULL || png == NULL || length == NULL) return 1;
    *png = NULL;
    *length = 0;
    __block int status = 0;
    dispatch_semaphore_t done = dispatch_semaphore_create(0);
    dispatch_block_t block = ^{
        WKWebView *view = (WKWebView *)native;
        WKSnapshotConfiguration *config = [[WKSnapshotConfiguration alloc] init];
        // CGRectNull leaves the capture rect unset, so WebKit captures the whole view bounds.
        // Include pending WebContent updates so a host capture cannot park a stale blank frame.
        config.rect = CGRectNull;
        config.afterScreenUpdates = YES;
        [view takeSnapshotWithConfiguration:config completionHandler:^(NSImage *image, NSError *error) {
            if (image == nil || error != nil) {
                status = 2;
                dispatch_semaphore_signal(done);
                return;
            }
            // Encoding is not the main thread's work.
            //
            // The completion handler is delivered on the main queue, and turning a window-sized
            // image into PNG there holds the thread that draws the window: measured 2026-08-17, a
            // window in front stopped drawing for 87ms on a layout change that took one picture.
            // The image is retained and handed to a background queue, and only the wait ends on the
            // main thread.
            [image retain];
            dispatch_async(dispatch_get_global_queue(QOS_CLASS_USER_INITIATED, 0), ^{
                CGImageRef cg = [image CGImageForProposedRect:NULL context:nil hints:nil];
                if (cg == NULL) {
                    status = 3;
                    [image release];
                    dispatch_semaphore_signal(done);
                    return;
                }
                NSBitmapImageRep *rep = [[NSBitmapImageRep alloc] initWithCGImage:cg];
                NSData *data = [rep representationUsingType:NSBitmapImageFileTypePNG properties:@{}];
                [rep release];
                if (data == nil) {
                    status = 4;
                    [image release];
                    dispatch_semaphore_signal(done);
                    return;
                }
                void *buffer = malloc(data.length);
                if (buffer == NULL) {
                    status = 5;
                    [image release];
                    dispatch_semaphore_signal(done);
                    return;
                }
                memcpy(buffer, data.bytes, data.length);
                *png = buffer;
                *length = (int)data.length;
                [image release];
                dispatch_semaphore_signal(done);
            });
        }];
        [config release];
    };
    // The snapshot's completion handler is delivered on the main queue, so waiting for it from the
    // main thread deadlocks the window. Refuse instead: the caller is a command handler on a
    // goroutine, and a call from the main thread is a wiring mistake that should say so rather than
    // freeze the application.
    if ([NSThread isMainThread]) return 7;
    dispatch_async(dispatch_get_main_queue(), block);
    // Bounded, so a wedged web process is an error rather than a capture that never returns.
    if (dispatch_semaphore_wait(done, dispatch_time(DISPATCH_TIME_NOW, 5 * NSEC_PER_SEC)) != 0) return 6;
    return status;
}
