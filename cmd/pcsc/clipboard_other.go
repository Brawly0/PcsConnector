//go:build !windows

package main

import (
	"os"
	"os/exec"
	"strings"
)

// Linux: shells out to wl-clipboard (Wayland) or xclip (X11).
// No change-notification here, so the watcher polls.

func clipSeq() (uint32, bool) { return 0, false }

func wayland() bool { return os.Getenv("WAYLAND_DISPLAY") != "" }

func clipRead() (string, error) {
	var out []byte
	var err error
	if wayland() {
		out, err = exec.Command("wl-paste", "-n").Output()
	} else {
		out, err = exec.Command("xclip", "-selection", "clipboard", "-o").Output()
	}
	if err != nil {
		return "", nil // empty clipboard exits non-zero; a missing tool is reported by `doctor`
	}
	return string(out), nil
}

func clipWrite(text string) error {
	var cmd *exec.Cmd
	if wayland() {
		cmd = exec.Command("wl-copy")
	} else {
		cmd = exec.Command("xclip", "-selection", "clipboard", "-i")
	}
	cmd.Stdin = strings.NewReader(text)
	return cmd.Run() // the tool forks into the background to own the selection
}
