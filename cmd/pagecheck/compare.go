package main

import (
	"context"
	_ "embed"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

//go:embed compare.html
var compareHTML []byte

// compareViews are the views -compare writes: name and the page's query.
var compareViews = []struct{ name, query string }{
	{"front", "az=0"}, {"three-quarter", "az=-30&el=15"}, {"left", "az=-90"}, {"right", "az=90"},
	{"back", "az=180"}, {"top", "el=60&zoom=0.7&aim=0.9"}, {"under-screen", "el=5&zoom=0.45&aim=0.35"},
	{"left-close", "az=-90&zoom=0.5&aim=0.6"}, {"back-close", "az=180&el=25&zoom=0.6&aim=0.5"},
}

// compare writes M5Stack's model (glb, e.g. ../StackChan/app/assets/stack_chan_model.glb) and
// ours (web/robot.glb) side by side from each of compareViews, as compare-<view>.png in out.
func compare(b *browser, glb, out string) error {
	m5, err := os.ReadFile(glb)
	if err != nil {
		return err
	}
	ours, err := os.ReadFile("web/robot.glb")
	if err != nil {
		return err
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) { w.Write(compareHTML) })
	mux.HandleFunc("GET /m5.glb", func(w http.ResponseWriter, r *http.Request) { w.Write(m5) })
	mux.HandleFunc("GET /robot.glb", func(w http.ResponseWriter, r *http.Request) { w.Write(ours) })
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return err
	}
	srv := &http.Server{Handler: mux}
	go srv.Serve(ln)
	defer srv.Close()
	site := "http://" + ln.Addr().String() + "/"

	for _, v := range compareViews {
		if err := func() error {
			ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
			defer cancel()
			p, err := openPage(b)
			if err != nil {
				return err
			}
			defer p.ws.Close()
			if err := p.setSize(ctx, width{w: 1200, h: 600}, 1200); err != nil {
				return err
			}
			if err := p.Call(ctx, "Page.navigate", map[string]any{"url": site + "?" + v.query}, nil); err != nil {
				return err
			}
			if err := p.waitFor(ctx, `document.title !== ''`, 45*time.Second); err != nil {
				return err
			}
			var title string
			p.Eval(ctx, `document.title`, &title)
			if strings.HasPrefix(title, "error") {
				return fmt.Errorf("%s", title)
			}
			shot, err := p.screenshot(ctx, map[string]any{"x": 0, "y": 0, "width": 1200, "height": 600, "scale": 1})
			if err != nil {
				return err
			}
			file := filepath.Join(out, "compare-"+v.name+".png")
			fmt.Printf("     compare %s: %s\n", v.name, file)
			return os.WriteFile(file, shot, 0o644)
		}(); err != nil {
			return fmt.Errorf("compare %s: %w", v.name, err)
		}
	}
	return nil
}
