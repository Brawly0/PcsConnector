//go:build !windows

package main

import "os/exec"

func hideWindow(c *exec.Cmd) {}

func openBrowser(url string) { exec.Command("xdg-open", url).Start() }
