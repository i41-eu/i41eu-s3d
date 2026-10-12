package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"time"
)

// cdp is one page's DevTools protocol session: commands answered by id, events to a handler.
type cdp struct {
	ws      *wsConn
	mu      sync.Mutex
	next    int
	waiting map[int]chan reply
	onEvent func(method string, params json.RawMessage)
	done    chan struct{}
	err     error
}

type reply struct {
	Result json.RawMessage `json:"result"`
	Error  *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

func newCDP(ws *wsConn, onEvent func(string, json.RawMessage)) *cdp {
	c := &cdp{ws: ws, waiting: map[int]chan reply{}, onEvent: onEvent, done: make(chan struct{})}
	go c.read()
	return c
}

func (c *cdp) read() {
	defer close(c.done)
	for {
		b, err := c.ws.ReadMessage()
		if err != nil {
			c.err = err
			return
		}
		var m struct {
			ID     int             `json:"id"`
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
			reply
		}
		if err := json.Unmarshal(b, &m); err != nil {
			continue
		}
		if m.ID == 0 {
			if m.Method != "" && c.onEvent != nil {
				c.onEvent(m.Method, m.Params)
			}
			continue
		}
		c.mu.Lock()
		ch := c.waiting[m.ID]
		delete(c.waiting, m.ID)
		c.mu.Unlock()
		if ch != nil {
			ch <- m.reply
		}
	}
}

// Call sends a command and decodes its result into out (nil: ignored).
func (c *cdp) Call(ctx context.Context, method string, params any, out any) error {
	c.mu.Lock()
	c.next++
	id := c.next
	ch := make(chan reply, 1)
	c.waiting[id] = ch
	c.mu.Unlock()
	if params == nil {
		params = struct{}{}
	}
	b, err := json.Marshal(map[string]any{"id": id, "method": method, "params": params})
	if err != nil {
		return err
	}
	if err := c.ws.WriteText(b); err != nil {
		return err
	}
	select {
	case r := <-ch:
		if r.Error != nil {
			return fmt.Errorf("%s: %s", method, r.Error.Message)
		}
		if out != nil {
			return json.Unmarshal(r.Result, out)
		}
		return nil
	case <-c.done:
		return fmt.Errorf("%s: connection lost: %v", method, c.err)
	case <-ctx.Done():
		return fmt.Errorf("%s: %w", method, ctx.Err())
	}
}

// Eval runs a JavaScript expression (awaiting a promise) and decodes its JSON value into out.
func (c *cdp) Eval(ctx context.Context, expr string, out any) error {
	var r struct {
		Result struct {
			Value json.RawMessage `json:"value"`
		} `json:"result"`
		ExceptionDetails *struct {
			Text      string `json:"text"`
			Exception struct {
				Description string `json:"description"`
			} `json:"exception"`
		} `json:"exceptionDetails"`
	}
	err := c.Call(ctx, "Runtime.evaluate", map[string]any{"expression": expr, "returnByValue": true, "awaitPromise": true}, &r)
	if err != nil {
		return err
	}
	if e := r.ExceptionDetails; e != nil {
		return fmt.Errorf("javascript: %s %s", e.Text, e.Exception.Description)
	}
	if out != nil {
		return json.Unmarshal(r.Result.Value, out)
	}
	return nil
}

// findChrome gives a Chrome or Chromium: the -chrome flag or $CHROME, then the usual names on
// PATH, then the usual places on macOS, Windows and Playwright's download.
func findChrome(flagged string) (string, error) {
	if flagged != "" {
		return flagged, nil
	}
	if p := os.Getenv("CHROME"); p != "" {
		return p, nil
	}
	for _, n := range []string{"google-chrome", "google-chrome-stable", "chromium", "chromium-browser", "chrome"} {
		if p, err := exec.LookPath(n); err == nil {
			return p, nil
		}
	}
	var places []string
	switch runtime.GOOS {
	case "darwin":
		places = []string{"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome", "/Applications/Chromium.app/Contents/MacOS/Chromium"}
	case "windows":
		places = []string{os.Getenv("ProgramFiles") + `\Google\Chrome\Application\chrome.exe`, os.Getenv("ProgramFiles(x86)") + `\Google\Chrome\Application\chrome.exe`}
	}
	pw := os.Getenv("PLAYWRIGHT_BROWSERS_PATH")
	if pw == "" {
		if h, err := os.UserHomeDir(); err == nil {
			pw = filepath.Join(h, ".cache", "ms-playwright")
		}
	}
	if m, _ := filepath.Glob(filepath.Join(pw, "chromium-*", "chrome-linux", "chrome")); len(m) > 0 {
		places = append(places, m[len(m)-1])
	}
	for _, p := range places {
		if _, err := os.Stat(p); err == nil {
			return p, nil
		}
	}
	return "", errors.New("no Chrome or Chromium found: install one, or give -chrome or $CHROME")
}

// browser is a headless Chrome started for the check, with its DevTools address.
type browser struct {
	cmd     *exec.Cmd
	dir     string
	devtool string // http://127.0.0.1:port
	exited  chan struct{}
	once    sync.Once
}

func startBrowser(chrome string) (*browser, error) {
	dir, err := os.MkdirTemp("", "pagecheck-chrome-")
	if err != nil {
		return nil, err
	}
	args := []string{
		"--headless=new", "--remote-debugging-port=0", "--remote-debugging-address=127.0.0.1",
		"--user-data-dir=" + dir, "--no-first-run", "--no-default-browser-check", "--hide-scrollbars",
		"--disable-background-networking", "--disable-component-update", "--disable-sync", "--no-pings",
		"--use-angle=swiftshader", "--enable-unsafe-swiftshader", // WebGL without a GPU
		"about:blank",
	}
	if runtime.GOOS == "linux" && os.Geteuid() == 0 {
		args = append([]string{"--no-sandbox"}, args...) // Chrome refuses root with its sandbox (containers)
	}
	cmd := exec.Command(chrome, args...)
	ownGroup(cmd)
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		os.RemoveAll(dir)
		return nil, err
	}
	b := &browser{cmd: cmd, dir: dir, exited: make(chan struct{})}
	go func() {
		cmd.Wait()
		close(b.exited)
	}()
	found := make(chan string, 1)
	go func() {
		re := regexp.MustCompile(`DevTools listening on (ws://\S+)`)
		s := bufio.NewScanner(stderr)
		for s.Scan() {
			if m := re.FindStringSubmatch(s.Text()); m != nil {
				found <- m[1]
				break
			}
		}
		io.Copy(io.Discard, stderr)
	}()
	select {
	case ws := <-found:
		b.devtool = "http://" + strings.SplitN(strings.TrimPrefix(ws, "ws://"), "/", 2)[0]
		onSignal(b.Close)
		if started != nil {
			started(b)
		}
		return b, nil
	case <-time.After(30 * time.Second):
		b.Close()
		return nil, errors.New("chrome did not start its DevTools in 30 s")
	}
}

// newPage opens a tab and connects to it.
func (b *browser) newPage(onEvent func(string, json.RawMessage)) (*cdp, error) {
	req, _ := http.NewRequest(http.MethodPut, b.devtool+"/json/new?about:blank", nil)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	var t struct {
		WebSocketDebuggerURL string `json:"webSocketDebuggerUrl"`
	}
	if err := json.NewDecoder(res.Body).Decode(&t); err != nil {
		return nil, fmt.Errorf("new tab: %w", err)
	}
	ws, err := wsDial(t.WebSocketDebuggerURL)
	if err != nil {
		return nil, err
	}
	return newCDP(ws, onEvent), nil
}

// Close ends Chrome and everything it started, and removes its profile: first as Chrome is
// built for (Browser.close), then by force: its process group (the helpers, which outlive the
// browser process) and its crash handlers (a session of their own). Safe to call more than once.
func (b *browser) Close() {
	b.once.Do(func() {
		if b.devtool != "" {
			b.closeBrowser(3 * time.Second)
		}
		select {
		case <-b.exited:
		case <-time.After(5 * time.Second):
		}
		killGroup(b.cmd.Process.Pid)
		killWithArg(b.dir)
		select {
		case <-b.exited:
		case <-time.After(5 * time.Second):
		}
		os.RemoveAll(b.dir)
	})
}

// closeBrowser asks Chrome to close (CDP Browser.close on the browser's own target).
func (b *browser) closeBrowser(limit time.Duration) {
	c := http.Client{Timeout: limit}
	res, err := c.Get(b.devtool + "/json/version")
	if err != nil {
		return
	}
	var v struct {
		WebSocketDebuggerURL string `json:"webSocketDebuggerUrl"`
	}
	err = json.NewDecoder(res.Body).Decode(&v)
	res.Body.Close()
	if err != nil {
		return
	}
	ws, err := wsDial(v.WebSocketDebuggerURL)
	if err != nil {
		return
	}
	defer ws.Close()
	ctx, cancel := context.WithTimeout(context.Background(), limit)
	defer cancel()
	newCDP(ws, func(string, json.RawMessage) {}).Call(ctx, "Browser.close", nil, nil)
}

// started is told each browser (tests).
var started func(*browser)
