package main

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/i41-eu/i41eu-assets/robot3d"
)

func load(t *testing.T) (Components, []App) {
	t.Helper()
	p, err := loadComponents(files)
	if err != nil {
		t.Fatal(err)
	}
	apps, err := loadApps(files)
	if err != nil {
		t.Fatal(err)
	}
	return p, apps
}

func checkSource(t *testing.T, what string, s Source) {
	t.Helper()
	if s.URL != "" {
		if !strings.HasPrefix(s.URL, "https://docs.m5stack.com/") || s.Repo != "" || s.Quote == "" {
			t.Errorf("%s: a docs source is an M5Stack docs page and a quote: %+v", what, s)
		}
		return
	}
	if s.Repo == "" || len(s.Ref) != 40 || s.Path == "" || s.Line < 1 || s.Quote == "" {
		t.Errorf("%s: incomplete source %+v", what, s)
	}
}

// The robot's part names in robot.glb: robot3d's and the screen.
func modelParts() map[string]robot3d.Part {
	m := map[string]robot3d.Part{}
	for _, p := range robot3d.Parts() {
		m[p.Name] = p
	}
	return m
}

// checkWhere: a place on one of robot3d's parts, within its bounds, with a shape and a source.
func checkWhere(t *testing.T, what string, w Where, model map[string]robot3d.Part) {
	t.Helper()
	if sh := w.Shape; sh != nil {
		ok := sh.Kind == "circle" && sh.R > 0 && sh.R < 40 || sh.Kind == "rect" && sh.W > 0 && sh.H > 0 && sh.W < 60 && sh.H < 60 && sh.R >= 0 && sh.R <= min(sh.W, sh.H)/2
		if !ok {
			t.Errorf("%s: shape %+v: a circle (r) or a rect (w, h, r up to half of the smaller)", what, *sh)
		}
	}
	if w.Area != "" && w.Area != "screen" {
		t.Errorf("%s: area %q", what, w.Area)
	}
	if w.Out != nil && (len(w.Out) != 3 || w.Out[0] == 0 && w.Out[1] == 0 && w.Out[2] == 0) {
		t.Errorf("%s: out %v: a direction", what, w.Out)
	}
	checkSource(t, what+" where", w.Source)
	for _, p := range w.See {
		if _, ok := model[p]; !ok {
			t.Errorf("%s: see %q is not one of robot3d's parts", what, p)
		}
	}
	switch w.How {
	case "exact", "joint", "inside", "near":
	default:
		t.Errorf("%s: how %q", what, w.How)
	}
	mp, ok := model[w.Part]
	if !ok {
		t.Errorf("%s: part %q is not one of robot3d's", what, w.Part)
		return
	}
	// The offset is on or in its part: within its bounds (0.5 mm to spare).
	lo, hi := mp.Positions[0], mp.Positions[0]
	for _, v := range mp.Positions {
		lo = robot3d.V3{X: min(lo.X, v.X), Y: min(lo.Y, v.Y), Z: min(lo.Z, v.Z)}
		hi = robot3d.V3{X: max(hi.X, v.X), Y: max(hi.Y, v.Y), Z: max(hi.Z, v.Z)}
	}
	o := w.Offset
	if o[0] < lo.X-0.5 || o[0] > hi.X+0.5 || o[1] < lo.Y-0.5 || o[1] > hi.Y+0.5 || o[2] < lo.Z-0.5 || o[2] > hi.Z+0.5 {
		t.Errorf("%s: offset %v outside %s (%+v to %+v)", what, o, w.Part, lo, hi)
	}
}

func kindOK(k string) bool { return k == "sensor" || k == "actuator" || k == "other" }

// Components: ids unique (a component's never a feature's, so a use names one or the other),
// feature ids unique across all of them, each "on" a window of its component, each exclude an
// existing feature that excludes this one too, every place on the model, every entry sourced.
func TestComponents(t *testing.T) {
	p, _ := load(t)
	if len(p.Components) == 0 {
		t.Fatal("no components")
	}
	model := modelParts()
	ids := map[string]string{} // id: "component" or "feature"
	excludes := map[string]map[string]bool{}
	for _, c := range p.Components {
		for _, id := range append([]string{c.ID}, featureIDs(c)...) {
			if id == "" || ids[id] != "" {
				t.Errorf("%s: id %q empty or twice", c.ID, id)
			}
			ids[id] = "feature"
		}
		ids[c.ID] = "component"
	}
	for _, c := range p.Components {
		if c.Name == "" || c.Chip == "" || !kindOK(c.Kind) || len(c.Features) == 0 {
			t.Errorf("%s: name, chip, kind and features are needed", c.ID)
		}
		if c.Where != nil && len(c.Windows) > 0 {
			t.Errorf("%s: a place of its own and windows", c.ID)
		}
		if c.Where != nil {
			checkWhere(t, c.ID, *c.Where, model)
		}
		windows := map[string]bool{}
		for _, w := range c.Windows {
			if w.ID == "" || w.Name == "" || windows[w.ID] {
				t.Errorf("%s: window %q: an id (once) and a name", c.ID, w.ID)
			}
			windows[w.ID] = true
			checkWhere(t, c.ID+"/"+w.ID, w.Where, model)
		}
		for _, s := range c.Sources {
			checkSource(t, c.ID, s)
		}
		for _, f := range c.Features {
			if f.Name == "" || f.Does == "" || !kindOK(f.Kind) || len(f.Sources) == 0 {
				t.Errorf("%s: name, kind, what it does and a source are needed", f.ID)
			}
			for _, s := range f.Sources {
				checkSource(t, f.ID, s)
			}
			for _, w := range f.On {
				if !windows[w] {
					t.Errorf("%s: on %q, not a window of %s", f.ID, w, c.ID)
				}
			}
			excludes[f.ID] = map[string]bool{}
			for _, x := range f.Excludes {
				if ids[x.Feature] != "feature" || x.Feature == f.ID {
					t.Errorf("%s: excludes %q, not another feature", f.ID, x.Feature)
				}
				checkSource(t, f.ID+" excludes", x.Source)
				excludes[f.ID][x.Feature] = true
			}
		}
	}
	for a, xs := range excludes {
		for b := range xs {
			if !excludes[b][a] {
				t.Errorf("%s excludes %s, but not the other way round", a, b)
			}
		}
	}
}

func featureIDs(c Component) []string {
	var out []string
	for _, f := range c.Features {
		out = append(out, f.ID)
	}
	return out
}

// The places taken from robot3d's exported values are those values.
func TestPlaces(t *testing.T) {
	p, _ := load(t)
	at := map[string][3]float64{} // a single-feature component's place, by its feature
	for _, c := range p.Components {
		if c.Where != nil && len(c.Features) == 1 {
			at[c.Features[0].ID] = c.Where.Offset
		}
	}
	c, _, _ := robot3d.Screen()
	for id, want := range map[string]robot3d.V3{"display": c, "touch-screen": c} {
		got := at[id]
		if math.Abs(got[0]-want.X)+math.Abs(got[1]-want.Y)+math.Abs(got[2]-want.Z) > 1e-9 {
			t.Errorf("%s at %v, robot3d %+v", id, got, want)
		}
	}

}

func TestApps(t *testing.T) {
	p, apps := load(t)
	ids := map[string]bool{} // features and components: a use names either
	for _, c := range p.Components {
		ids[c.ID] = true
		for _, f := range c.Features {
			ids[f.ID] = true
		}
	}
	got := map[string]bool{}
	for _, a := range apps {
		got[a.ID] = true
		if a.Page != "https://s.i41.eu/"+a.ID || a.Web == "" {
			t.Errorf("%s: page %q (want https://s.i41.eu/%s) and web %q are needed", a.ID, a.Page, a.ID, a.Web)
		}
		if a.Name == "" || a.About == "" || a.Repo == "" || len(a.Sources) == 0 || len(a.Uses) == 0 {
			t.Errorf("%s: name, about, repo, sources and uses are needed", a.ID)
		}
		for _, s := range a.Sources {
			checkSource(t, a.ID, s)
		}
		if l := a.LEDs; l != nil {
			if !regexp.MustCompile(`^#[0-9a-f]{6}$`).MatchString(l.Color) || l.Does == "" || len(l.Sources) == 0 {
				t.Errorf("%s: leds %+v: a #rrggbb colour, what it shows and a source are needed", a.ID, l)
			}
			if l.Each != "" {
				each := strings.Split(l.Each, ",")
				if m := regexp.MustCompile(`^(#[0-9a-f]{6})\*12$`).FindStringSubmatch(l.Each); m != nil {
					each = []string{m[1]}
				} else if len(each) != 12 {
					t.Errorf("%s: leds each %q: 12 LEDs, not %d", a.ID, l.Each, len(each))
				}
				for _, c := range each {
					if c != "" && c != l.Color {
						t.Errorf("%s: leds each %q: %q, its colour is %s", a.ID, l.Each, c, l.Color)
					}
				}
			}
			for _, s := range l.Sources {
				checkSource(t, a.ID+" leds", s)
			}
		}
		if sc := a.Screen; sc != nil {
			if _, err := files.ReadFile("web/screens/" + sc.Image); err != nil || len(sc.Sources) == 0 {
				t.Errorf("%s: screen %q: in web/screens/, with a source", a.ID, sc.Image)
			}
			for _, s := range sc.Sources {
				checkSource(t, a.ID+" screen", s)
			}
		}
		used := map[string]bool{}
		for _, u := range a.Uses {
			if !ids[u.Feature] {
				t.Errorf("%s: uses %q, not a feature or a component of components.json", a.ID, u.Feature)
			}
			if used[u.Feature] {
				t.Errorf("%s: %s twice", a.ID, u.Feature)
			}
			used[u.Feature] = true
			if u.Does == "" || len(u.Sources) == 0 {
				t.Errorf("%s %s: what it does and a source are needed", a.ID, u.Feature)
			}
			for _, s := range u.Sources {
				checkSource(t, a.ID+" "+u.Feature, s)
			}
		}
	}
	for _, id := range []string{"pet", "focus", "raw", "dev"} {
		if !got[id] {
			t.Errorf("app %s missing", id)
		}
	}
}

// Every source's quote is on its line, at its commit, in the repos cloned beside this one
// (setup.sh); skipped where a repo or git is not there.
func TestSourcesQuoted(t *testing.T) {
	p, apps := load(t)
	var all []Source
	for _, c := range p.Components {
		all = append(all, c.Sources...)
		if c.Where != nil {
			all = append(all, c.Where.Source)
		}
		for _, w := range c.Windows {
			all = append(all, w.Where.Source)
		}
		for _, f := range c.Features {
			all = append(all, f.Sources...)
			for _, x := range f.Excludes {
				all = append(all, x.Source)
			}
		}
	}
	for _, a := range apps {
		all = append(all, a.Sources...)
		if a.LEDs != nil {
			all = append(all, a.LEDs.Sources...)
		}
		for _, u := range a.Uses {
			all = append(all, u.Sources...)
		}
	}
	cache := map[string][]string{}
	checked := 0
	for _, s := range all {
		if s.URL != "" { // a docs page: not in a repo
			continue
		}
		dir := filepath.Join("..", s.Repo)
		if _, err := os.Stat(filepath.Join(dir, ".git")); err != nil {
			continue
		}
		k := s.Repo + "@" + s.Ref + ":" + s.Path
		lines, ok := cache[k]
		if !ok {
			out, err := exec.Command("git", "-C", dir, "show", s.Ref+":"+s.Path).Output()
			if err != nil {
				t.Errorf("%s: %v", k, err)
				cache[k] = nil
				continue
			}
			lines = strings.Split(string(out), "\n")
			cache[k] = lines
		}
		if lines == nil {
			continue
		}
		if s.Line > len(lines) || !strings.Contains(lines[s.Line-1], s.Quote) {
			t.Errorf("%s line %d: %q not there", k, s.Line, s.Quote)
		}
		checked++
	}
	if checked == 0 {
		t.Skip("no related repos beside this one (setup.sh)")
	}
	t.Logf("%d of %d sources checked against the repos beside this one", checked, len(all))
}

func TestServe(t *testing.T) {
	h, err := handler()
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(h)
	defer srv.Close()
	for p, want := range map[string]int{
		"/": 200, "/app.js": 200, "/robot.glb": 200, "/data/components.json": 200, "/data/parts.json": 404, "/data/apps.json": 200,
		"/healthz": 200, "/three@0.160.0/build/three.module.js": 200, "/three@0.160.0/examples/jsm/loaders/GLTFLoader.js": 200, "/three@0.160.0/examples/jsm/utils/BufferGeometryUtils.js": 200, "/three/build/three.module.js": 404, "/three@0.160.0/LICENSE": 404, "/main.go": 404, "/web/index.html": 404, "/data/apps/pet.json": 404,
	} {
		res, err := http.Get(srv.URL + p)
		if err != nil {
			t.Fatal(err)
		}
		b, _ := io.ReadAll(res.Body)
		res.Body.Close()
		if res.StatusCode != want {
			t.Errorf("%s: %d, want %d", p, res.StatusCode, want)
		}
		switch p {
		case "/":
			csp := res.Header.Get("Content-Security-Policy")
			if !strings.Contains(csp, "'sha256-") || strings.Contains(csp, "jsdelivr") {
				t.Errorf("CSP %q", csp)
			}
		case "/data/apps.json":
			var v struct{ Apps []App }
			if err := json.Unmarshal(b, &v); err != nil || len(v.Apps) < 2 {
				t.Errorf("apps.json: %v, %d apps", err, len(v.Apps))
			}
		case "/robot.glb":
			if len(b) < 12 || string(b[:4]) != "glTF" {
				t.Error("robot.glb: not a glTF binary")
			}
		}
	}
}

// Each file is checked again on each load (no-cache) and, unchanged, answered with a 304.
func TestServeRevalidates(t *testing.T) {
	h, err := handler()
	if err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/robot.glb", nil))
	etag := rec.Header().Get("ETag")
	if rec.Code != 200 || rec.Header().Get("Cache-Control") != "no-cache" || etag == "" {
		t.Fatalf("robot.glb: %d, Cache-Control %q, ETag %q", rec.Code, rec.Header().Get("Cache-Control"), etag)
	}
	req := httptest.NewRequest("GET", "/robot.glb", nil)
	req.Header.Set("If-None-Match", etag)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotModified || rec.Body.Len() != 0 {
		t.Errorf("robot.glb again with its ETag: %d, %d bytes", rec.Code, rec.Body.Len())
	}
}

// Big files go gzipped to a browser that takes it (three.js 1.27 MB, the model 2.4 MB), plain to
// one that does not; three.js at its versioned path is kept a year, the rest checked each load.
func TestServeGzip(t *testing.T) {
	h, err := handler()
	if err != nil {
		t.Fatal(err)
	}
	index, _ := files.ReadFile("web/index.html")
	// relative, so the page also works under a path (s.i41.eu/s3d/)
	if !strings.Contains(string(index), `"./`+threeVersion+`/build/three.module.js"`) {
		t.Fatalf("the import map does not use ./%s/", threeVersion)
	}
	for path, cache := range map[string]string{"/" + threeVersion + "/build/three.module.js": "public, max-age=31536000, immutable", "/robot.glb": "no-cache", "/app.js": "no-cache"} {
		req := httptest.NewRequest("GET", path, nil)
		req.Header.Set("Accept-Encoding", "gzip, deflate, br")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Header().Get("Content-Encoding") != "gzip" || rec.Header().Get("Cache-Control") != cache || rec.Header().Get("Vary") != "Accept-Encoding" {
			t.Errorf("%s: %v", path, rec.Header())
			continue
		}
		zipped := rec.Body.Len()
		zr, err := gzip.NewReader(rec.Body)
		if err != nil {
			t.Fatal(err)
		}
		plain, _ := io.ReadAll(zr)
		rec2 := httptest.NewRecorder()
		h.ServeHTTP(rec2, httptest.NewRequest("GET", path, nil))
		if rec2.Header().Get("Content-Encoding") != "" || !bytes.Equal(rec2.Body.Bytes(), plain) {
			t.Errorf("%s: plain and gzipped differ", path)
		}
		t.Logf("%s: %d bytes, %d gzipped", path, len(plain), zipped)
		req3 := httptest.NewRequest("GET", path, nil)
		req3.Header.Set("Accept-Encoding", "gzip")
		req3.Header.Set("If-None-Match", rec.Header().Get("ETag"))
		rec3 := httptest.NewRecorder()
		h.ServeHTTP(rec3, req3)
		if rec3.Code != http.StatusNotModified {
			t.Errorf("%s again with its gzip ETag: %d", path, rec3.Code)
		}
	}
}
