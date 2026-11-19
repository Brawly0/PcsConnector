//go:build windows

package main

import (
	"log"
	"os"
	"os/exec"
	"strings"
	"syscall"
)

func hideWindow(c *exec.Cmd) { c.SysProcAttr = &syscall.SysProcAttr{HideWindow: true} }

const runKey = `HKCU\Software\Microsoft\Windows\CurrentVersion\Run`

// autostartExe is this exe, unless it is a temporary build (go run / go test), which must not autostart.
func autostartExe() string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	low := strings.ToLower(exe)
	if strings.Contains(low, `\temp\`) || strings.Contains(low, "go-build") {
		return ""
	}
	return exe
}

// autostartOn reports whether PcsConnector starts (hidden) when you sign in to Windows.
func autostartOn() bool {
	q := exec.Command("reg", "query", runKey, "/v", "PcsConnector")
	hideWindow(q)
	return q.Run() == nil
}

// setAutostart adds or removes the HKCU Run entry. Starting at sign-in means a reboot never leaves the
// laptop with nothing to connect to.
func setAutostart(on bool) error {
	if !on {
		c := exec.Command("reg", "delete", runKey, "/v", "PcsConnector", "/f")
		hideWindow(c)
		c.Run()
		return nil
	}
	exe := autostartExe()
	if exe == "" {
		return nil
	}
	val := `"` + exe + `" background`
	q := exec.Command("reg", "query", runKey, "/v", "PcsConnector")
	hideWindow(q)
	if out, err := q.Output(); err == nil && strings.Contains(string(out), val) {
		return nil
	}
	c := exec.Command("reg", "add", runKey, "/v", "PcsConnector", "/t", "REG_SZ", "/d", val, "/f")
	hideWindow(c)
	if err := c.Run(); err != nil {
		return err
	}
	log.Printf("PcsConnector will start by itself when you sign in (turn this off in the window's settings)")
	return nil
}

// openApp shows url in a standalone app window (Edge/Chrome --app), falling back to the default browser.
func openApp(url string) {
	for _, p := range []string{
		os.Getenv("ProgramFiles(x86)") + `\Microsoft\Edge\Application\msedge.exe`,
		os.Getenv("ProgramFiles") + `\Microsoft\Edge\Application\msedge.exe`,
		os.Getenv("ProgramFiles") + `\Google\Chrome\Application\chrome.exe`,
	} {
		if _, err := os.Stat(p); err == nil {
			if exec.Command(p, "--app="+url, "--window-size=520,760").Start() == nil {
				return
			}
		}
	}
	exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Start()
}

// openAppFull shows url in a fullscreen app window.
func openAppFull(url string) {
	for _, p := range []string{
		os.Getenv("ProgramFiles(x86)") + `\Microsoft\Edge\Application\msedge.exe`,
		os.Getenv("ProgramFiles") + `\Microsoft\Edge\Application\msedge.exe`,
		os.Getenv("ProgramFiles") + `\Google\Chrome\Application\chrome.exe`,
	} {
		if _, err := os.Stat(p); err == nil {
			if exec.Command(p, "--app="+url, "--start-fullscreen").Start() == nil {
				return
			}
		}
	}
	exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Start()
}
