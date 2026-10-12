//go:build linux

package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// TestMain runs pagecheck's main in a child process when PAGECHECK_TEST_INFO is set: against a
// site of its own that never answers, telling the test Chrome's pid and profile in that file.
func TestMain(m *testing.M) {
	if info, ok := os.LookupEnv("PAGECHECK_TEST_INFO"); ok {
		started = func(b *browser) {
			os.WriteFile(info, []byte(strconv.Itoa(b.cmd.Process.Pid)+"\n"+b.dir), 0o644)
		}
		site := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			time.Sleep(time.Minute)
		}))
		os.Args = []string{"pagecheck", "-url", site.URL, "-o", os.TempDir()}
		main()
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func needChrome(t *testing.T) string {
	if testing.Short() {
		t.Skip("starts Chrome")
	}
	chrome, err := findChrome("")
	if err != nil {
		t.Skip(err)
	}
	return chrome
}

// left is what remains of a Chrome started as pid with the profile dir: its process group's
// members and the processes with the profile in their arguments, after up to 3 s.
func left(pid int, dir string) []string {
	var found []string
	for end := time.Now().Add(3 * time.Second); ; time.Sleep(100 * time.Millisecond) {
		found = nil
		stats, _ := filepath.Glob("/proc/[0-9]*/stat")
		for _, f := range stats {
			b, err := os.ReadFile(f)
			if err != nil {
				continue
			}
			// pid (comm) state ppid pgrp ...: comm may hold spaces, so read after the last ")"
			s := string(b)
			comm := s[strings.Index(s, "(")+1 : strings.LastIndex(s, ")")]
			fields := strings.Fields(s[strings.LastIndex(s, ")")+1:])
			if len(fields) > 2 && fields[0] != "Z" && fields[2] == strconv.Itoa(pid) {
				found = append(found, comm+" "+filepath.Base(filepath.Dir(f)))
			}
		}
		for _, p := range pidsWithArg(dir) {
			found = append(found, "with the profile: "+strconv.Itoa(p))
		}
		if len(found) == 0 || time.Now().After(end) {
			return found
		}
	}
}

// Close ends the browser and all its helpers, and removes the profile.
func TestCloseLeavesNothing(t *testing.T) {
	b, err := startBrowser(needChrome(t))
	if err != nil {
		t.Fatal(err)
	}
	p, err := b.newPage(func(string, json.RawMessage) {})
	if err != nil {
		b.Close()
		t.Fatal(err)
	}
	var v any
	if err := p.Eval(context.Background(), "1+1", &v); err != nil || v != float64(2) {
		t.Errorf("eval: %v %v", v, err)
	}
	pid, dir := b.cmd.Process.Pid, b.dir
	if n := len(left(pid, "\x00never\x00")); n < 3 {
		t.Errorf("only %d processes in Chrome's group: not the group it should be", n)
	}
	b.Close()
	b.Close() // again: nothing happens
	if l := left(pid, dir); len(l) > 0 {
		t.Errorf("left after Close: %v", l)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Errorf("the profile %s is still there", dir)
	}
}

// A signal while pagecheck checks a site ends it, with its Chrome.
func TestSignalLeavesNothing(t *testing.T) {
	needChrome(t)
	info := filepath.Join(t.TempDir(), "info")
	cmd := exec.Command(os.Args[0], "-test.run=^$")
	cmd.Env = append(os.Environ(), "PAGECHECK_TEST_INFO="+info)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	var pid int
	var dir string
	for end := time.Now().Add(30 * time.Second); time.Now().Before(end) && pid == 0; time.Sleep(100 * time.Millisecond) {
		if b, err := os.ReadFile(info); err == nil {
			p, d, _ := strings.Cut(string(b), "\n")
			pid, _ = strconv.Atoi(p)
			dir = d
		}
	}
	if pid == 0 {
		cmd.Process.Kill()
		t.Fatal("Chrome did not start")
	}
	time.Sleep(time.Second)
	cmd.Process.Signal(syscall.SIGTERM)
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case <-done:
	case <-time.After(30 * time.Second):
		cmd.Process.Kill()
		killGroup(pid)
		t.Fatal("pagecheck did not end")
	}
	if got := cmd.ProcessState.ExitCode(); got != 1 {
		t.Errorf("exit code %d, want 1", got)
	}
	if l := left(pid, dir); len(l) > 0 {
		killGroup(pid)
		killWithArg(dir)
		t.Errorf("left after pagecheck ended: %v", l)
	}
}
