// Command pagecheck checks the page in a headless Chrome or Chromium over the DevTools protocol
// (standard library only): it builds and starts the site on a free port (or checks -url), opens
// it at 1200 and 420 px and checks that
//
//   - the robot is drawn and fills most of the view, also after a resize;
//   - the labels are plain names (a place with two parts lists both), and a tap on one picks its part;
//   - no two labels overlap while the robot turns;
//   - the side panel's links wrap: nothing overflows the panel or the page;
//   - there are no console errors or uncaught exceptions.
//
// It writes a screenshot of each width to a temporary directory (or -o) and prints its path;
// -views also writes the 3D view alone at 1200 px from fixed views (the page's ?view=…: front,
// back, left, right, and close-ups of the head and the LED bars), for comparing with photos.
// Run from the repository's root:
//
//	go run ./cmd/pagecheck [-chrome path] [-url http://…] [-o dir] [-views all|front,leds,…]
//	go run ./cmd/pagecheck -compare ../StackChan/app/assets/stack_chan_model.glb [-o dir]
//
// -compare only draws M5Stack's own model of the robot (in M5Stack's StackChan app) and ours side
// by side from the same views (compare-<view>.png), to check ours against it.
package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"flag"
	"fmt"
	"image"
	"image/png"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

type width struct {
	name         string
	w, h         int
	mobile       bool
	resizedWidth int // checked again at this width
}

var widths = []width{
	{name: "1200", w: 1200, h: 800, resizedWidth: 840},
	{name: "420", w: 420, h: 860, mobile: true, resizedWidth: 320},
}

// minFill: the robot's drawn bounds cover at least this much of the 3D view's height or width.
const minFill = 0.6

func main() {
	chromeFlag := flag.String("chrome", "", "Chrome or Chromium (default: $CHROME, then the usual names and places)")
	siteURL := flag.String("url", "", "check this site (default: build and start this repository's site)")
	out := flag.String("o", "", "the directory for the screenshots (default: a new temporary one)")
	compareFlag := flag.String("compare", "", "only write M5Stack's model (this glb, e.g. ../StackChan/app/assets/stack_chan_model.glb) and ours side by side from fixed views")
	viewsFlag := flag.String("views", "", "also screenshot these fixed views (comma-separated, or all: "+strings.Join(allViews, ",")+"; view#app with an app's LEDs)")
	flag.Parse()
	handleSignals()

	views := strings.Split(*viewsFlag, ",")
	switch *viewsFlag {
	case "":
		views = nil
	case "all":
		views = allViews
	}
	if *compareFlag != "" {
		if err := runCompare(*chromeFlag, *compareFlag, *out); err != nil {
			fmt.Fprintln(os.Stderr, "pagecheck:", err)
			os.Exit(2)
		}
		return
	}
	failed, err := run(*chromeFlag, *siteURL, *out, views)
	if err != nil {
		fmt.Fprintln(os.Stderr, "pagecheck:", err)
		os.Exit(2)
	}
	if failed > 0 {
		fmt.Printf("pagecheck: %d checks failed\n", failed)
		os.Exit(1)
	}
	fmt.Println("pagecheck: all checks passed")
}

func runCompare(chromeFlag, glb, out string) error {
	chrome, err := findChrome(chromeFlag)
	if err != nil {
		return err
	}
	if out == "" {
		if out, err = os.MkdirTemp("", "pagecheck-"); err != nil {
			return err
		}
	}
	b, err := startBrowser(chrome)
	if err != nil {
		return err
	}
	defer b.Close()
	return compare(b, glb, out)
}

// allViews are the page's fixed views (web/app.js: views).
var allViews = []string{"front", "back", "left", "right", "head", "leds", "top", "photo", "seam", "holes", "topseam", "under", "bottom", "led"}

func run(chromeFlag, siteURL, out string, views []string) (int, error) {
	chrome, err := findChrome(chromeFlag)
	if err != nil {
		return 0, err
	}
	if out == "" {
		if out, err = os.MkdirTemp("", "pagecheck-"); err != nil {
			return 0, err
		}
	}
	if siteURL == "" {
		stop, u, err := startSite()
		if err != nil {
			return 0, err
		}
		stop = sync.OnceFunc(stop)
		onSignal(stop)
		defer stop()
		siteURL = u
	}
	b, err := startBrowser(chrome)
	if err != nil {
		return 0, err
	}
	defer b.Close()
	fmt.Printf("pagecheck: %s with %s\n", siteURL, chrome)

	failed := 0
	for _, w := range widths {
		n, err := checkWidth(b, siteURL, out, w)
		if err != nil {
			fmt.Printf("FAIL %s px: %v\n", w.name, err)
			n++
		}
		failed += n
	}
	for _, v := range views {
		if err := shootView(b, siteURL, out, v); err != nil {
			fmt.Printf("FAIL view %s: %v\n", v, err)
			failed++
		}
	}
	return failed, nil
}

// shootView writes the 3D view alone (no markers, no hint) at 1200 px from a fixed view; a name
// like leds#focus shows an app (its LEDs).
func shootView(b *browser, site, out, name string) error {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	p, err := openPage(b)
	if err != nil {
		return err
	}
	defer p.ws.Close()
	if err := p.setSize(ctx, widths[0], widths[0].w); err != nil {
		return err
	}
	if err := p.Call(ctx, "Page.navigate", map[string]any{"url": site + "/?view=" + name}, nil); err != nil {
		return err
	}
	time.Sleep(500 * time.Millisecond)
	if err := p.waitFor(ctx, `!document.getElementById('status')`, 45*time.Second); err != nil {
		return err
	}
	var st rect
	if err := p.Eval(ctx, `(() => {
		const box = document.getElementById('markers');
		if (box.checked) { box.checked = false; box.dispatchEvent(new Event('change')); }
		document.getElementById('hint').style.visibility = 'hidden';
		const r = document.getElementById('stage').getBoundingClientRect();
		return {X: r.left + scrollX, Y: r.top + scrollY, W: r.width, H: r.height};
	})()`, &st); err != nil {
		return err
	}
	time.Sleep(800 * time.Millisecond)
	shot, err := p.screenshot(ctx, map[string]any{"x": st.X, "y": st.Y, "width": st.W, "height": st.H, "scale": 1})
	if err != nil {
		return err
	}
	file := filepath.Join(out, "view-"+strings.ReplaceAll(name, "#", "-")+".png")
	if err := os.WriteFile(file, shot, 0o644); err != nil {
		return err
	}
	p.mu.Lock()
	errs := append([]string(nil), p.errors...)
	p.mu.Unlock()
	if len(errs) > 0 {
		return fmt.Errorf("console errors: %s", strings.Join(errs, "; "))
	}
	fmt.Printf("     view %s: screenshot %s\n", name, file)
	return nil
}

// onSignal adds what a SIGINT, SIGTERM or SIGHUP ends before pagecheck exits (os.Exit skips the
// defers): each browser (startBrowser), the site it started.
func onSignal(f func()) {
	sigMu.Lock()
	sigStops = append(sigStops, f)
	sigMu.Unlock()
}

var (
	sigMu    sync.Mutex
	sigStops []func()
)

func handleSignals() {
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)
	go func() {
		s := <-ch
		fmt.Fprintln(os.Stderr, "pagecheck:", s)
		sigMu.Lock()
		stops := sigStops
		sigMu.Unlock()
		for i := len(stops) - 1; i >= 0; i-- {
			stops[i]()
		}
		os.Exit(1)
	}()
}

// startSite builds this repository's site and starts it on a free local port.
func startSite() (stop func(), url string, err error) {
	dir, err := os.MkdirTemp("", "pagecheck-site-")
	if err != nil {
		return nil, "", err
	}
	bin := filepath.Join(dir, "i41eu-s3d")
	build := exec.Command("go", "build", "-o", bin, ".")
	build.Stdout, build.Stderr = os.Stderr, os.Stderr
	if err := build.Run(); err != nil {
		os.RemoveAll(dir)
		return nil, "", fmt.Errorf("go build (run pagecheck from the repository's root): %w", err)
	}
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, "", err
	}
	addr := l.Addr().String()
	l.Close()
	cmd := exec.Command(bin, "-listen", addr)
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		return nil, "", err
	}
	stop = func() { cmd.Process.Kill(); cmd.Wait(); os.RemoveAll(dir) }
	url = "http://" + addr
	for i := 0; i < 100; i++ {
		if res, err := http.Get(url + "/healthz"); err == nil {
			res.Body.Close()
			return stop, url, nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	stop()
	return nil, "", fmt.Errorf("the site did not answer at %s", url)
}

// page is a tab with what it logged.
type page struct {
	*cdp
	mu     sync.Mutex
	errors []string
}

func (p *page) logError(s string) {
	p.mu.Lock()
	p.errors = append(p.errors, s)
	p.mu.Unlock()
}

func openPage(b *browser) (*page, error) {
	p := &page{}
	var c *cdp
	onEvent := func(method string, params json.RawMessage) {
		switch method {
		case "Runtime.exceptionThrown":
			var e struct {
				ExceptionDetails struct {
					Text      string `json:"text"`
					Exception struct {
						Description string `json:"description"`
					} `json:"exception"`
				} `json:"exceptionDetails"`
			}
			json.Unmarshal(params, &e)
			p.logError("exception: " + e.ExceptionDetails.Text + " " + e.ExceptionDetails.Exception.Description)
		case "Runtime.consoleAPICalled":
			var e struct {
				Type string `json:"type"`
				Args []struct {
					Value       any    `json:"value"`
					Description string `json:"description"`
				} `json:"args"`
			}
			json.Unmarshal(params, &e)
			if e.Type == "error" || e.Type == "assert" {
				var parts []string
				for _, a := range e.Args {
					if a.Description != "" {
						parts = append(parts, a.Description)
					} else {
						parts = append(parts, fmt.Sprint(a.Value))
					}
				}
				p.logError("console." + e.Type + ": " + strings.Join(parts, " "))
			}
		case "Log.entryAdded":
			var e struct {
				Entry struct {
					Level string `json:"level"`
					Text  string `json:"text"`
					URL   string `json:"url"`
				} `json:"entry"`
			}
			json.Unmarshal(params, &e)
			if e.Entry.Level == "error" {
				p.logError("log: " + e.Entry.Text + " " + e.Entry.URL)
			}
		}
	}
	c, err := b.newPage(onEvent)
	if err != nil {
		return nil, err
	}
	p.cdp = c
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	for _, m := range []string{"Runtime.enable", "Log.enable", "Page.enable"} {
		if err := c.Call(ctx, m, nil, nil); err != nil {
			return nil, err
		}
	}
	return p, nil
}

func (p *page) setSize(ctx context.Context, w width, px int) error {
	if err := p.Call(ctx, "Emulation.setDeviceMetricsOverride", map[string]any{"width": px, "height": w.h, "deviceScaleFactor": 1, "mobile": w.mobile}, nil); err != nil {
		return err
	}
	return p.Call(ctx, "Emulation.setTouchEmulationEnabled", map[string]any{"enabled": w.mobile, "maxTouchPoints": 5}, nil)
}

// waitFor evaluates a boolean expression until it is true.
func (p *page) waitFor(ctx context.Context, expr string, limit time.Duration) error {
	end := time.Now().Add(limit)
	for {
		var ok bool
		err := p.Eval(ctx, expr, &ok) // fails while the page is still being replaced: tried again
		if err == nil && ok {
			return nil
		}
		if time.Now().After(end) {
			if err != nil {
				return err
			}
			return fmt.Errorf("not within %s: %s", limit, expr)
		}
		time.Sleep(200 * time.Millisecond)
	}
}

func (p *page) screenshot(ctx context.Context, clip map[string]any) ([]byte, error) {
	var r struct {
		Data string `json:"data"`
	}
	params := map[string]any{"format": "png", "captureBeyondViewport": true}
	if clip != nil {
		params["clip"] = clip
	}
	if err := p.Call(ctx, "Page.captureScreenshot", params, &r); err != nil {
		return nil, err
	}
	return base64.StdEncoding.DecodeString(r.Data)
}

type rect struct{ X, Y, W, H float64 }

// robotFill gives how much of the 3D view's height and width the robot's drawing covers: the
// markers and the hint hidden, a screenshot of the view, the bounds of what differs from its
// background.
func (p *page) robotFill(ctx context.Context) (fh, fw float64, err error) {
	var st rect
	err = p.Eval(ctx, `(() => {
		const box = document.getElementById('markers');
		if (box.checked) { box.checked = false; box.dispatchEvent(new Event('change')); }
		document.getElementById('hint').style.visibility = 'hidden';
		const r = document.getElementById('stage').getBoundingClientRect();
		return {X: r.left + scrollX, Y: r.top + scrollY, W: r.width, H: r.height};
	})()`, &st)
	if err != nil {
		return 0, 0, err
	}
	time.Sleep(300 * time.Millisecond)
	b, err := p.screenshot(ctx, map[string]any{"x": st.X, "y": st.Y, "width": st.W, "height": st.H, "scale": 1})
	restore := `(() => { const box = document.getElementById('markers'); box.checked = true; box.dispatchEvent(new Event('change'));
		document.getElementById('hint').style.visibility = ''; return true; })()`
	if e := p.Eval(ctx, restore, nil); err == nil {
		err = e
	}
	if err != nil {
		return 0, 0, err
	}
	img, err := png.Decode(bytes.NewReader(b))
	if err != nil {
		return 0, 0, err
	}
	box := drawnBounds(img)
	bw, bh := img.Bounds().Dx(), img.Bounds().Dy()
	return float64(box.Dy()) / float64(bh), float64(box.Dx()) / float64(bw), nil
}

// drawnBounds is the box of the pixels that differ from the background (taken a few pixels in
// from the middle of the left edge), 14 px inside the edges (the view's rounded border).
func drawnBounds(img image.Image) image.Rectangle {
	r := img.Bounds()
	br, bg, bb, _ := img.At(r.Min.X+4, (r.Min.Y+r.Max.Y)/2).RGBA()
	box := image.Rectangle{}
	diff := func(a, b uint32) int {
		d := int(a>>8) - int(b>>8)
		if d < 0 {
			return -d
		}
		return d
	}
	for y := r.Min.Y + 14; y < r.Max.Y-14; y++ { // inside the view's rounded border
		for x := r.Min.X + 14; x < r.Max.X-14; x++ {
			cr, cg, cb, _ := img.At(x, y).RGBA()
			if diff(cr, br)+diff(cg, bg)+diff(cb, bb) > 24 {
				box = box.Union(image.Rect(x, y, x+1, y+1))
			}
		}
	}
	return box
}

// labelsJS gives the visible labels with their boxes and the side panel's overflow.
const labelsJS = `(() => {
	const ls = [...document.querySelectorAll('.label')].filter((e) => e.offsetParent && getComputedStyle(e.parentElement).display !== 'none')
		.map((e) => { const r = e.getBoundingClientRect(); return {text: e.textContent, x: r.left, y: r.top, w: r.width, h: r.height}; });
	const aside = document.querySelector('aside'), ar = aside.getBoundingClientRect();
	const wide = [...aside.querySelectorAll('a, .src, .does, .about, .meta')].filter((e) => e.getBoundingClientRect().right > ar.right + 1).map((e) => e.textContent.slice(0, 60));
	const st = document.getElementById('stage'), sr = st.getBoundingClientRect();
	const shapes = (st.shapeBoxes || []).map((b) => ({text: b.text, x: sr.left + b.x0, y: sr.top + b.y0, w: b.x1 - b.x0, h: b.y1 - b.y0}));
	return {labels: ls, shapes, asideOverflow: aside.scrollWidth - aside.clientWidth, pageOverflow: document.documentElement.scrollWidth - innerWidth, wide};
})()`

type label struct {
	Text       string
	X, Y, W, H float64
}

type layout struct {
	Labels        []label
	Shapes        []label // each shown part's outline (its box on the screen), by its label's text
	AsideOverflow float64
	PageOverflow  float64
	Wide          []string
}

func checkWidth(b *browser, site, out string, w width) (failed int, err error) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	p, err := openPage(b)
	if err != nil {
		return 0, err
	}
	defer p.ws.Close()
	report := func(ok bool, what string, args ...any) {
		state := "ok  "
		if !ok {
			state = "FAIL"
			failed++
		}
		fmt.Printf("%s %s px: %s\n", state, w.name, fmt.Sprintf(what, args...))
	}

	if err := p.setSize(ctx, w, w.w); err != nil {
		return 0, err
	}
	if err := p.Call(ctx, "Page.navigate", map[string]any{"url": site + "/#pet"}, nil); err != nil {
		return 0, err
	}
	time.Sleep(500 * time.Millisecond)
	loaded := p.waitFor(ctx, `!document.getElementById('status') || document.getElementById('status').textContent.startsWith('Could not')`, 45*time.Second)
	var status string
	p.Eval(ctx, `(document.getElementById('status') || {}).textContent || ''`, &status)
	report(loaded == nil && status == "", "the robot loaded%s", errText(loaded, status))
	if loaded != nil || status != "" {
		return failed, nil
	}
	time.Sleep(1500 * time.Millisecond)

	// The robot drawn, filling the view.
	fh, fw, err := p.robotFill(ctx)
	if err != nil {
		return failed, err
	}
	report(max(fh, fw) >= minFill, "the robot drawn: %.0f%% of the view's height, %.0f%% of its width (at least %.0f%% of one)", 100*fh, 100*fw, 100*minFill)

	// The labels plain names; no labels overlapping as it turns; nothing overflowing.
	seen := map[string]bool{} // the labels seen in any frame (only the sides facing the camera show)
	overlaps := map[string]bool{}
	overShape := map[string]bool{} // a label over another part's outline
	var l layout
	for frame := 0; frame < 10; frame++ {
		if err := p.Eval(ctx, labelsJS, &l); err != nil {
			return failed, err
		}
		for _, lb := range l.Labels {
			seen[lb.Text] = true
			for _, sh := range l.Shapes {
				if sh.Text != lb.Text && lb.X < sh.X+sh.W-1 && sh.X+1 < lb.X+lb.W && lb.Y < sh.Y+sh.H-1 && sh.Y+1 < lb.Y+lb.H {
					overShape[fmt.Sprintf("%q over %q", lb.Text, sh.Text)] = true
				}
			}
		}
		for i, a := range l.Labels {
			for _, c := range l.Labels[i+1:] {
				if a.X < c.X+c.W && c.X < a.X+a.W && a.Y < c.Y+c.H && c.Y < a.Y+a.H {
					overlaps[fmt.Sprintf("%s %.0f,%.0f %.0fx%.0f / %s %.0f,%.0f %.0fx%.0f", a.Text, a.X, a.Y, a.W, a.H, c.Text, c.X, c.Y, c.W, c.H)] = true
				}
			}
		}
		time.Sleep(600 * time.Millisecond)
	}
	counted := regexp.MustCompile(`\d+ parts`)
	var bad []string
	for t := range seen {
		if counted.MatchString(t) {
			bad = append(bad, t)
		}
	}
	report(len(seen) > 0 && len(bad) == 0, "labels plain names: %q%s", keys(seen), listText(" a count:", bad))
	report(len(overlaps) == 0, "no labels overlap in 10 frames as it turns%s", listText(":", keys(overlaps)))
	report(len(overShape) == 0, "no label over another part's outline in 10 frames%s", listText(":", keys(overShape)))
	var overButtons []string
	p.Eval(ctx, `(() => { const bs = ['turn-left', 'turn-right', 'spin'].map((id) => document.getElementById(id).getBoundingClientRect());
	  return [...document.querySelectorAll('.label')].filter((e) => e.offsetParent && getComputedStyle(e.parentElement).display !== 'none')
	    .filter((e) => { const r = e.getBoundingClientRect(); return bs.some((b) => r.left < b.right && b.left < r.right && r.top < b.bottom && b.top < r.bottom); })
	    .map((e) => e.textContent); })()`, &overButtons)
	report(len(overButtons) == 0, "no label under the turn arrows or the Turn button%s", listText(":", overButtons))
	report(l.AsideOverflow <= 0 && l.PageOverflow <= 0 && len(l.Wide) == 0, "links wrap: the panel overflows by %.0f px, the page by %.0f px%s", l.AsideOverflow, l.PageOverflow, listText(" wider than the panel:", l.Wide))

	// A pick in the list (Camera) turns the front to the camera.
	p.Eval(ctx, `(() => { const li = document.querySelector('li[data-id="camera"] .name'); if (li) li.click(); return true; })()`, nil)
	time.Sleep(1200 * time.Millisecond)

	// A tap on a label picks its part: focused, and its row in the list marked. The light sensor's
	// place has two parts: its label lists both names.
	p.waitFor(ctx, `[...document.querySelectorAll('.label')].some((e) => e.textContent === 'Ambient light, Proximity' && e.offsetParent && getComputedStyle(e.parentElement).display !== 'none')`, 4*time.Second)
	var g struct {
		Text string
		X, Y float64
	}
	if err := p.Eval(ctx, `(() => { const e = [...document.querySelectorAll('.label')].find((e) => e.textContent.startsWith('Ambient light') && e.offsetParent && getComputedStyle(e.parentElement).display !== 'none'); if (!e) return {Text: ''}; const r = e.getBoundingClientRect(); return {Text: e.textContent, X: r.left + r.width / 2, Y: r.top + r.height / 2}; })()`, &g); err != nil {
		return failed, err
	}
	for _, t := range []string{"mousePressed", "mouseReleased"} {
		if g.Text == "" {
			break
		}
		if err := p.Call(ctx, "Input.dispatchMouseEvent", map[string]any{"type": t, "x": g.X, "y": g.Y, "button": "left", "clickCount": 1}, nil); err != nil {
			return failed, err
		}
	}
	time.Sleep(300 * time.Millisecond)
	var row string
	p.Eval(ctx, `(document.querySelector('li.active .name') || {}).textContent || ''`, &row)
	report(g.Text == "Ambient light, Proximity" && row == "Ambient light", "a tap on the label %q picks its part: %q", g.Text, row)

	// The screenshot, then the robot framed again after a resize.
	shot, err := p.screenshot(ctx, nil)
	if err != nil {
		return failed, err
	}
	file := filepath.Join(out, "pagecheck-"+w.name+".png")
	if err := os.WriteFile(file, shot, 0o644); err != nil {
		return failed, err
	}
	fmt.Printf("     %s px: screenshot %s\n", w.name, file)
	p.Eval(ctx, `(() => { const e = document.getElementById('pop'); if (e && !e.hidden) document.getElementById('stage').click(); return true; })()`, nil)

	// One column (a phone): a feature's "Show on the model" link, far down the list, picks it and
	// scrolls back up to the model; on a desktop the link is hidden.
	var up struct {
		Shown, StageTop, Active string
	}
	p.Eval(ctx, `(() => { const a = document.querySelector('li[data-id="proximity"] .tomodel'); if (!a) return {Shown: 'none'};
	  a.scrollIntoView(); return {Shown: getComputedStyle(a).display}; })()`, &up)
	if w.mobile {
		p.Eval(ctx, `(() => { document.querySelector('li[data-id="proximity"] .tomodel').click(); return true; })()`, nil)
		p.waitFor(ctx, `(() => { const t = Math.round(document.getElementById('stage').getBoundingClientRect().top); const ok = t === window._lastTop && t > -40 && t < 120; window._lastTop = t; return ok; })()`, 4*time.Second) // the smooth scroll settled
		p.Eval(ctx, `({Shown: 'inline-block', StageTop: String(Math.round(document.getElementById('stage').getBoundingClientRect().top)),
		  Active: (document.querySelector('li.active .name') || {}).textContent || ''})`, &up)
		top, _ := strconv.Atoi(up.StageTop)
		report(top > -40 && top < 120 && up.Active == "Proximity", "\"Show on the model\" scrolls up to the model (its top at %s px) and picks %q", up.StageTop, up.Active)
		p.Eval(ctx, `(() => { window.scrollTo(0, 0); return true; })()`, nil)
	} else {
		report(up.Shown == "none", "\"Show on the model\" hidden beside the model: %s", up.Shown)
	}

	// A pick in the list stops the turning and focuses its marker; All clears and checks the
	// list's markers; the app comes from the address (#raw).
	var pick struct {
		Spin  string
		Focus string
	}
	p.Eval(ctx, `(() => { const li = document.querySelector('li[data-id="pitch-servo"] .name'); if (li) li.click(); return true; })()`, nil)
	time.Sleep(1200 * time.Millisecond)
	p.Eval(ctx, `({Spin: document.getElementById('spin').getAttribute('aria-pressed'), Focus: (document.querySelector('.label.focus') || {}).textContent || ''})`, &pick)
	report(pick.Spin == "false" && pick.Focus == "Head pitch servo", "a pick in the list (Head pitch servo): turning %s, focused %q", map[string]string{"false": "stopped", "true": "still on"}[pick.Spin], pick.Focus)
	if shot, err := p.screenshot(ctx, nil); err == nil {
		os.WriteFile(filepath.Join(out, "pagecheck-"+w.name+"-pick.png"), shot, 0o644)
	}
	// A second click on the picked row unchecks it; its checkbox picks it again.
	var again struct {
		Checked bool
		Focus   string
	}
	p.Eval(ctx, `(() => { document.querySelector('li[data-id="pitch-servo"] .name').click(); return true; })()`, nil)
	time.Sleep(300 * time.Millisecond)
	p.Eval(ctx, `({Checked: document.querySelector('li[data-id="pitch-servo"] input').checked})`, &again)
	unchecked := !again.Checked
	p.Eval(ctx, `(() => { document.querySelector('li[data-id="pitch-servo"] input').click(); return true; })()`, nil)
	p.waitFor(ctx, `[...document.querySelectorAll('.label.focus')].some((e) => e.offsetParent && getComputedStyle(e.parentElement).display !== 'none')`, 3*time.Second)
	p.Eval(ctx, `({Checked: document.querySelector('li[data-id="pitch-servo"] input').checked, Focus: (document.querySelector('.label.focus') || {}).textContent || ''})`, &again)
	report(unchecked && again.Checked && again.Focus == "Head pitch servo", "a second click unchecks the row (%v), its checkbox picks it again: focused %q", unchecked, again.Focus)

	// A part inside another (the IMU, in the CoreS3) is shown through it.
	var inside string
	p.Eval(ctx, `(() => { document.querySelector('li[data-id="imu"] .name').click(); return true; })()`, nil)
	p.waitFor(ctx, `[...document.querySelectorAll('.label.focus')].some((e) => e.offsetParent && getComputedStyle(e.parentElement).display !== 'none')`, 3*time.Second)
	time.Sleep(400 * time.Millisecond)
	p.Eval(ctx, `(document.querySelector('.label.focus') || {}).textContent || ''`, &inside)
	report(strings.HasPrefix(inside, "Accelerometer and gyroscope"), "a pick of the IMU (inside the CoreS3) shows it: focused %q", inside)
	if shot, err := p.screenshot(ctx, nil); err == nil {
		os.WriteFile(filepath.Join(out, "pagecheck-"+w.name+"-inside.png"), shot, 0o644)
	}
	// A pick of a part on the bottom (the power LED, in Raw data) tilts the view to show it from below.
	var under struct{ Focus string }
	p.Eval(ctx, `(() => { location.hash = 'raw'; return true; })()`, nil)
	time.Sleep(500 * time.Millisecond)
	p.Eval(ctx, `(() => { const li = document.querySelector('li[data-id="power-led"] .name'); if (li) li.click(); return true; })()`, nil)
	p.waitFor(ctx, `[...document.querySelectorAll('.label.focus')].some((e) => e.offsetParent && getComputedStyle(e.parentElement).display !== 'none')`, 3*time.Second)
	p.Eval(ctx, `({Focus: [...document.querySelectorAll('.label.focus')].filter((e) => e.offsetParent && getComputedStyle(e.parentElement).display !== 'none').map((e) => e.textContent).join(',')})`, &under)
	report(under.Focus == "Power/charge LED", "a pick of the power LED shows it from below: focused %q", under.Focus)
	if shot, err := p.screenshot(ctx, nil); err == nil {
		os.WriteFile(filepath.Join(out, "pagecheck-"+w.name+"-under.png"), shot, 0o644)
	}
	p.Eval(ctx, `(() => { location.hash = 'pet'; return true; })()`, nil)
	time.Sleep(500 * time.Millisecond)

	// Every entry of Raw data, picked, shows its part on the robot (its label focused, in view).
	if w.name == "1200" {
		p.Eval(ctx, `(() => { location.hash = 'raw'; return true; })()`, nil)
		time.Sleep(500 * time.Millisecond)
		var ids []string
		p.Eval(ctx, `[...document.querySelectorAll('#panel li[data-id]')].map((li) => li.dataset.id)`, &ids)
		var dead []string
		for _, id := range ids {
			p.Eval(ctx, `(() => { const n = document.querySelector('li[data-id="`+id+`"] .name'); if (n) n.click(); return true; })()`, nil)
			shown := `[...document.querySelectorAll('.label.focus')].some((e) => e.offsetParent && getComputedStyle(e.parentElement).display !== 'none')`
			// software WebGL under a CPU cap can be slow: a second, longer chance before it counts as missing
			if p.waitFor(ctx, shown, 3*time.Second) != nil && p.waitFor(ctx, shown, 6*time.Second) != nil {
				dead = append(dead, id)
			}
		}
		report(len(ids) > 20 && len(dead) == 0, "every Raw data entry (%d) shows its part when picked%s", len(ids), listText(" not:", dead))
		p.Eval(ctx, `(() => { location.hash = 'pet'; return true; })()`, nil)
		time.Sleep(500 * time.Millisecond)
	}

	var shown [2]int
	p.Eval(ctx, `(() => { const b = document.getElementById('markers'); const n = () => [...document.querySelectorAll('.label')].filter((e) => e.textContent && e.offsetParent && getComputedStyle(e.parentElement).display !== 'none').length;
		b.checked = false; b.dispatchEvent(new Event('change')); const off = n(); b.checked = true; b.dispatchEvent(new Event('change')); return [off, n()]; })()`, &shown)
	report(shown[0] == 0 && shown[1] > 0, "All: %d labels cleared, %d checked", shown[0], shown[1])
	var raw string
	p.Eval(ctx, `(() => { location.hash = 'raw'; return true; })()`, nil)
	time.Sleep(400 * time.Millisecond)
	p.Eval(ctx, `(document.querySelector('#panel h2') || {}).textContent || ''`, &raw)
	report(raw == "Raw data", "#raw opens the app: %q", raw)
	p.Eval(ctx, `(() => { location.hash = 'pet'; return true; })()`, nil)
	time.Sleep(400 * time.Millisecond)

	if err := p.setSize(ctx, w, w.resizedWidth); err != nil {
		return failed, err
	}
	time.Sleep(800 * time.Millisecond)
	if fh, fw, err = p.robotFill(ctx); err != nil {
		return failed, err
	}
	report(max(fh, fw) >= minFill, "resized to %d px: the robot fills %.0f%% of the view's height, %.0f%% of its width", w.resizedWidth, 100*fh, 100*fw)

	p.mu.Lock()
	errs := append([]string(nil), p.errors...)
	p.mu.Unlock()
	report(len(errs) == 0, "no console errors%s", listText(":", errs))
	// ?pick= takes a component too: in an app, its row (the pet's head touch), the address as the
	// app pages on s.i41.eu link it.
	if err := p.Call(ctx, "Page.navigate", map[string]any{"url": site + "/?pick=si12t#pet"}, nil); err != nil {
		return failed, err
	}
	p.waitFor(ctx, `(document.querySelector('li.active .name') || {}).textContent === 'Head touch'`, 8*time.Second)
	var picked string
	p.Eval(ctx, `(document.querySelector('li.active .name') || {}).textContent || ''`, &picked)
	report(picked == "Head touch", "?pick=si12t#pet picks the pet's head touch: %q", picked)
	// A link to a part shows only that part (from s.i41.eu's app pages): ?pick=rgb-leds#raw checks
	// both LED strips and nothing else, and only their labels show.
	if err := p.Call(ctx, "Page.navigate", map[string]any{"url": site + "/?pick=rgb-leds#raw"}, nil); err != nil {
		return failed, err
	}
	p.waitFor(ctx, `(document.querySelector('li.active .name') || {}).textContent === 'RGB LEDs, left'`, 8*time.Second)
	var only struct {
		Checked []string
		Labels  []string
	}
	p.Eval(ctx, `({Checked: [...document.querySelectorAll('#panel input[data-id]')].filter((c) => c.checked).map((c) => c.dataset.id),
	  Labels: [...document.querySelectorAll('.label')].filter((e) => e.offsetParent && getComputedStyle(e.parentElement).display !== 'none' && e.textContent).map((e) => e.textContent)})`, &only)
	ledsOnly := len(only.Checked) == 2 && only.Checked[0] == "leds-left" && only.Checked[1] == "leds-right"
	for _, l := range only.Labels {
		ledsOnly = ledsOnly && strings.HasPrefix(l, "RGB LEDs")
	}
	report(ledsOnly, "?pick=rgb-leds#raw shows only the LED strips: checked %v, labels %v", only.Checked, only.Labels)
	return failed, nil
}

func errText(err error, status string) string {
	switch {
	case status != "":
		return ": " + status
	case err != nil:
		return ": " + err.Error()
	}
	return ""
}

func listText(prefix string, items []string) string {
	if len(items) == 0 {
		return ""
	}
	return prefix + " " + strings.Join(items, "; ")
}

func keys(m map[string]bool) []string {
	var k []string
	for s := range m {
		k = append(k, s)
	}
	return k
}
