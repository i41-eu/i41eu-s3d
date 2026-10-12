// Command i41eu-s3d serves s.i41.eu/s3d: the Stackchan robot in 3D (web/index.html, web/app.js,
// web/robot.glb from cmd/s3dgen), its components and their features (data/components.json) and what each app
// does with them (data/apps/*.json, served together as data/apps.json). Everything is embedded.
package main

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"embed"
	"encoding/base64"
	"encoding/json"
	"flag"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"path"
	"regexp"
	"sort"
	"strings"
	"time"
)

//go:embed web/index.html web/app.js web/robot.glb web/screens/*.png web/three data/components.json data/apps/*.json
var files embed.FS

// Source is where an entry is described: a line of a file in one of the related repos
// (github.com/i41-eu/<repo>, at commit ref); quote is a piece of that line.
//
// Or a page of M5Stack's docs (url, under https://docs.m5stack.com/), e.g. the annotated picture
// of the robot there; quote is what it says (a label on the picture).
type Source struct {
	Repo  string `json:"repo,omitempty"`
	Ref   string `json:"ref,omitempty"`
	Path  string `json:"path,omitempty"`
	Line  int    `json:"line,omitempty"`
	URL   string `json:"url,omitempty"`
	Quote string `json:"quote"`
}

// Where is a place on the robot: a part of robot.glb and an offset in it (millimetres,
// robot3d's rest space).
type Where struct {
	Part   string     `json:"part"`
	Offset [3]float64 `json:"offset"`
	How    string     `json:"how"`             // exact, joint, inside, near
	Area   string     `json:"area,omitempty"`  // screen: the screen's rectangle, outlined
	Out    []float64  `json:"out,omitempty"`   // from where its marker is seen (at rest); none: out from the yaw axis
	Shape  *Shape     `json:"shape,omitempty"` // its outline on the robot; none: a dot
	See    []string   `json:"see,omitempty"`   // parts turned see-through while it is picked (what it shows lies inside them)
	Note   string     `json:"note"`
	Source Source     `json:"source"`
}

// Shape is a part's outline on the robot (mm), on its surface facing out: a circle (r), or a
// rect (w across, h up, corners rounded by r). Free: at the offset itself, not on the surface
// (e.g. a ring round the turntable).
type Shape struct {
	Kind string  `json:"kind"` // circle, rect
	R    float64 `json:"r,omitempty"`
	W    float64 `json:"w,omitempty"`
	H    float64 `json:"h,omitempty"`
	Free bool    `json:"free,omitempty"`
}

// Component is one physical part or chip of the robot: where it is (one place, or windows: its
// sub-places, e.g. the light sensor's two dots, the head touch's three zones) and its features.
type Component struct {
	ID       string    `json:"id"`
	Name     string    `json:"name"`
	Chip     string    `json:"chip"`
	Kind     string    `json:"kind"`              // sensor, actuator, other: what it mostly is
	Where    *Where    `json:"where,omitempty"`   // nil: in its windows, or the docs do not say where
	Windows  []Window  `json:"windows,omitempty"` // its sub-places
	Features []Feature `json:"features"`
	Sources  []Source  `json:"sources,omitempty"`
}

// Window is a place of a component of its own (ids are unique within the component).
type Window struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Where Where  `json:"where"`
}

// Feature is what software gets from a component: a sensor's readings, an actuator's commands.
// Feature ids are unique across all components, so apps point at one.
type Feature struct {
	ID       string    `json:"id"`
	Name     string    `json:"name"`
	Kind     string    `json:"kind"` // sensor, actuator, other
	Does     string    `json:"does"`
	On       []string  `json:"on,omitempty"`       // the windows it works through; none: the whole component
	Excludes []Exclude `json:"excludes,omitempty"` // features that cannot run at the same time
	Sources  []Source  `json:"sources"`
}

// Exclude: a feature that cannot run at the same time as this one (listed on both), and why.
type Exclude struct {
	Feature string `json:"feature"`
	Source  Source `json:"source"`
}

// Components is data/components.json.
type Components struct {
	About      string      `json:"about"`
	Components []Component `json:"components"`
}

// Use is what an app does with a feature (or with all features of a component: its id).
type Use struct {
	Feature string   `json:"feature"`
	Does    string   `json:"does"`
	Sources []Source `json:"sources"`
}

// LEDs is the colour an app's LEDs show on the page (#rrggbb), and why. Each, if given, is each
// LED's colour in robot3d's -leds form (i41eu-assets renders.json): "#rrggbb*12", or 12
// comma-separated colours numbered as the robot's leds command does (left 0-5, right 6-11; 0 and
// 11 at the front), empty for an unlit LED; Color is then the colour of its lit ones.
type LEDs struct {
	Color   string   `json:"color"`
	Each    string   `json:"each,omitempty"`
	Does    string   `json:"does"`
	Sources []Source `json:"sources"`
}

// Screen is the picture an app shows on the robot's screen (web/screens/, from i41eu-assets'
// screens/), and why.
type Screen struct {
	Image   string   `json:"image"`
	Sources []Source `json:"sources"`
}

// App is data/apps/<id>.json.
type App struct {
	ID      string   `json:"id"`
	Name    string   `json:"name"`
	About   string   `json:"about"`
	Repo    string   `json:"repo"`
	Page    string   `json:"page"` // its page on s.i41.eu (https://s.i41.eu/<id>)
	Web     string   `json:"web,omitempty"`
	Sources []Source `json:"sources"`
	LEDs    *LEDs    `json:"leds,omitempty"`   // nil: the page's default
	Screen  *Screen  `json:"screen,omitempty"` // nil: the robot's launcher
	Uses    []Use    `json:"uses"`
}

// loadComponents reads data/components.json.
func loadComponents(fsys fs.FS) (Components, error) {
	var p Components
	b, err := fs.ReadFile(fsys, "data/components.json")
	if err == nil {
		err = strictJSON(b, &p)
	}
	return p, err
}

// loadApps reads data/apps/*.json, sorted by id.
func loadApps(fsys fs.FS) ([]App, error) {
	names, err := fs.Glob(fsys, "data/apps/*.json")
	if err != nil {
		return nil, err
	}
	var apps []App
	for _, n := range names {
		b, err := fs.ReadFile(fsys, n)
		if err != nil {
			return nil, err
		}
		var a App
		if err := strictJSON(b, &a); err != nil {
			return nil, fmt.Errorf("%s: %w", n, err)
		}
		if a.ID+".json" != path.Base(n) {
			return nil, fmt.Errorf("%s: id %q", n, a.ID)
		}
		apps = append(apps, a)
	}
	sort.Slice(apps, func(i, j int) bool { return apps[i].ID < apps[j].ID })
	return apps, nil
}

func strictJSON(b []byte, v any) error {
	d := json.NewDecoder(bytes.NewReader(b))
	d.DisallowUnknownFields()
	return d.Decode(v)
}

// threeVersion is three.js's path on the site (web/three: npm three@0.160.0); the import map uses it.
const threeVersion = "three@0.160.0"

// importMapHash is the CSP hash of the page's inline import map (three.js from web/three).
func importMapHash(page []byte) (string, error) {
	m := regexp.MustCompile(`(?s)<script type="importmap">(.*?)</script>`).FindSubmatch(page)
	if m == nil {
		return "", fmt.Errorf("index.html: no import map")
	}
	sum := sha256.Sum256(m[1])
	return "'sha256-" + base64.StdEncoding.EncodeToString(sum[:]) + "'", nil
}

func handler() (http.Handler, error) {
	if _, err := loadComponents(files); err != nil {
		return nil, fmt.Errorf("data/components.json: %w", err)
	}
	apps, err := loadApps(files)
	if err != nil {
		return nil, err
	}
	appsJSON, err := json.Marshal(map[string]any{"apps": apps})
	if err != nil {
		return nil, err
	}
	index, _ := files.ReadFile("web/index.html")
	hash, err := importMapHash(index)
	if err != nil {
		return nil, err
	}
	csp := strings.Join([]string{
		"default-src 'none'",
		"script-src 'self' " + hash, // three.js from here too (web/three): no third party sees a visitor
		"style-src 'unsafe-inline'",
		"img-src 'self' data: blob:",
		"connect-src 'self' data: blob:",
		"base-uri 'none'", "form-action 'none'", "frame-ancestors 'none'",
	}, "; ")

	// Every file is checked again on each load (no-cache, with an ETag: unchanged, a 304), so a
	// release shows at once, never an old robot.glb from a browser's cache.
	// serveCached answers a file: gzipped (once, here) for a browser that takes it, with an ETag
	// (unchanged: a 304). cache is its Cache-Control.
	serveCached := func(body []byte, ct, cache string) http.HandlerFunc {
		sum := sha256.Sum256(body)
		etag := `"` + base64.RawURLEncoding.EncodeToString(sum[:12]) + `"`
		var gz []byte
		if ct != "image/png" && len(body) > 1024 { // text, JSON, scripts, the model; PNGs are compressed
			var buf bytes.Buffer
			zw, _ := gzip.NewWriterLevel(&buf, gzip.BestCompression)
			zw.Write(body)
			zw.Close()
			gz = buf.Bytes()
		}
		return func(w http.ResponseWriter, r *http.Request) {
			h := w.Header()
			h.Set("Content-Type", ct)
			h.Set("Cache-Control", cache)
			h.Set("X-Content-Type-Options", "nosniff")
			if strings.HasPrefix(ct, "text/html") {
				h.Set("Content-Security-Policy", csp)
				h.Set("Referrer-Policy", "strict-origin-when-cross-origin")
			}
			out, tag := body, etag
			if gz != nil {
				h.Set("Vary", "Accept-Encoding")
				if strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") {
					out, tag = gz, etag[:len(etag)-1]+`-gz"`
					h.Set("Content-Encoding", "gzip")
				}
			}
			h.Set("ETag", tag)
			if r.Header.Get("If-None-Match") == tag {
				w.WriteHeader(http.StatusNotModified)
				return
			}
			w.Write(out)
		}
	}
	// serve: checked again on each load (no-cache), so a release shows at once.
	serve := func(body []byte, ct string) http.HandlerFunc { return serveCached(body, ct, "no-cache") }
	read := func(name string) []byte {
		b, _ := files.ReadFile(name)
		return b
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", serve(index, "text/html; charset=utf-8"))
	mux.HandleFunc("GET /app.js", serve(read("web/app.js"), "text/javascript; charset=utf-8"))
	// three.js at a versioned path: it never changes there, so a browser keeps it a year.
	scripts, _ := fs.Glob(files, "web/three/*/*.js")
	more, _ := fs.Glob(files, "web/three/examples/jsm/*/*.js")
	for _, f := range append(scripts, more...) {
		mux.HandleFunc("GET /"+threeVersion+"/"+strings.TrimPrefix(f, "web/three/"),
			serveCached(read(f), "text/javascript; charset=utf-8", "public, max-age=31536000, immutable"))
	}
	mux.HandleFunc("GET /robot.glb", serve(read("web/robot.glb"), "model/gltf-binary"))
	shots, _ := fs.Glob(files, "web/screens/*.png")
	for _, f := range shots {
		mux.HandleFunc("GET /screens/"+path.Base(f), serve(read(f), "image/png"))
	}
	mux.HandleFunc("GET /data/components.json", serve(read("data/components.json"), "application/json"))
	mux.HandleFunc("GET /data/apps.json", serve(appsJSON, "application/json"))
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("ok\n")) })
	return mux, nil
}

func main() {
	listen := flag.String("listen", ":8080", "HTTP listen address")
	flag.Parse()
	log := slog.New(slog.NewTextHandler(os.Stderr, nil))

	h, err := handler()
	if err != nil {
		log.Error("data", "err", err)
		os.Exit(1)
	}
	srv := &http.Server{
		Addr:              *listen,
		Handler:           h,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	log.Info("i41eu-s3d listening", "listen", *listen)
	if err := srv.ListenAndServe(); err != nil {
		log.Error("server stopped", "err", err)
		os.Exit(1)
	}
}
