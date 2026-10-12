//go:build !linux

package main

import (
	"os"
	"os/exec"
)

// ownGroup: process groups as on Linux are not used here; Close kills Chrome itself.
func ownGroup(cmd *exec.Cmd) {}

func killGroup(pid int) {
	if p, err := os.FindProcess(pid); err == nil {
		p.Kill()
	}
}

func killWithArg(s string) {}
