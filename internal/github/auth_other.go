//go:build !windows

package github

import (
	"os/exec"
	"runtime"
)

const macOS = runtime.GOOS == "darwin"

func hideWindow(*exec.Cmd) {}
