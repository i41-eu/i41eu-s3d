//go:build linux

package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"syscall"
)

// ownGroup starts Chrome in a process group of its own (its helpers and the google-chrome
// launcher's cat pipes join it), so killGroup reaches them all; and Chrome gets SIGKILL when
// pageshot dies without a chance to close it.
func ownGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true, Pdeathsig: syscall.SIGKILL}
}

// killGroup kills the process group that ownGroup made for the process pid.
func killGroup(pid int) {
	syscall.Kill(-pid, syscall.SIGKILL)
}

// killWithArg kills this user's processes whose command line contains s: Chrome's crash
// handlers start a session of their own, so killGroup misses them; they carry the profile
// directory in their arguments.
func killWithArg(s string) {
	for _, pid := range pidsWithArg(s) {
		syscall.Kill(pid, syscall.SIGKILL)
	}
}

// pidsWithArg lists the processes whose command line contains s (a zombie's is empty).
func pidsWithArg(s string) []int {
	var pids []int
	dirs, _ := filepath.Glob("/proc/[0-9]*")
	for _, d := range dirs {
		pid, err := strconv.Atoi(filepath.Base(d))
		if err != nil || pid == os.Getpid() {
			continue
		}
		cmdline, err := os.ReadFile(d + "/cmdline")
		if err != nil || !bytes.Contains(cmdline, []byte(s)) {
			continue
		}
		pids = append(pids, pid)
	}
	return pids
}
