package main

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

const usage = `pcsc - PcsConnector
  pcsc                        open the app window (pairing, files, screen)
  pcsc token                  create/show the shared secret
  pcsc host [--port N]        run on the Windows PC
  pcsc join [ip] [--port N]   run on the laptop (no ip: find the PC with the same code on this network)
  pcsc background             run without opening the window (used at login)
  pcsc doctor                 check required tools and permissions
  pcsc version                print the build version
options: --token T  --no-clipboard      env: PCSC_TOKEN, PCSC_HOME`

// version is stamped by build.ps1 (-X main.version=...).
var version = "dev"

func homeDirs() dirs {
	home := os.Getenv("PCSC_HOME")
	if home == "" {
		h, _ := os.UserHomeDir()
		home = filepath.Join(h, "PcsConnector")
	}
	return dirs{home, filepath.Join(home, "Inbox"), filepath.Join(home, "Outbox"), filepath.Join(home, "Sent")}
}

func readConfigToken(d dirs) string {
	b, err := os.ReadFile(filepath.Join(d.home, "config.json"))
	if err != nil {
		return ""
	}
	var c struct{ Token string }
	json.Unmarshal(b, &c)
	return c.Token
}

func main() {
	log.SetFlags(log.Ltime)
	if len(os.Args) < 2 {
		runUI(homeDirs(), 24800, true) // double-click: open the app window
		return
	}
	cmd, args := os.Args[1], os.Args[2:]
	d := homeDirs()

	fs := flag.NewFlagSet(cmd, flag.ExitOnError)
	port := fs.Int("port", 24800, "TCP port")
	token := fs.String("token", "", "shared secret")
	noClip := fs.Bool("no-clipboard", false, "disable clipboard sync")

	// allow `join <ip> --token T` (flag pkg stops at the first positional)
	var positional string
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		positional, args = args[0], args[1:]
	}
	fs.Parse(args)

	tok := *token
	if tok == "" {
		tok = os.Getenv("PCSC_TOKEN")
	}
	if tok == "" {
		tok = readConfigToken(d)
	}
	if c := normCode(tok); checkCode(c) == nil {
		tok = c // a pairing code typed in any case, with or without the dash
	}
	need := func() {
		if tok == "" {
			log.Fatal("No token. Run `pcsc token` on one machine, then pass the same value with --token on the other.")
		}
	}

	switch cmd {
	case "ui":
		runUI(d, *port, true)
	case "background":
		runUI(d, *port, false)
	case "version", "--version", "-v":
		fmt.Println("pcsc", version)
	case "token":
		if tok == "" {
			b := make([]byte, 18)
			rand.Read(b)
			tok = base64.RawURLEncoding.EncodeToString(b)
			os.MkdirAll(d.home, 0o755)
			j, _ := json.Marshal(map[string]string{"token": tok})
			os.WriteFile(filepath.Join(d.home, "config.json"), j, 0o600)
		}
		fmt.Printf("Shared token (use the same on both machines):\n  %s\n", tok)
	case "host":
		need()
		a, err := newApp(d, tok, *noClip)
		if err != nil {
			log.Fatal(err)
		}
		log.Fatal(a.serve(*port))
	case "join":
		need()
		a, err := newApp(d, tok, *noClip)
		if err != nil {
			log.Fatal(err)
		}
		a.dial(positional, *port)
	case "doctor":
		doctor()
	default:
		fmt.Println(usage)
	}
}

func doctor() {
	var need []string
	if runtime.GOOS != "windows" {
		if os.Getenv("WAYLAND_DISPLAY") != "" {
			need = []string{"wl-paste", "wl-copy"}
		} else {
			need = []string{"xclip"}
		}
	}
	fmt.Printf("pcsc %s  platform: %s/%s\n", version, runtime.GOOS, runtime.GOARCH)
	bad := false
	report := func(ok bool, okMsg, badMsg string) {
		if ok {
			fmt.Printf("ok      %s\n", okMsg)
		} else {
			bad = true
			fmt.Printf("PROBLEM %s\n", badMsg)
		}
	}
	for _, c := range need {
		_, err := exec.LookPath(c)
		report(err == nil, c, c+" is missing (clipboard sync): run install.sh")
	}
	if runtime.GOOS == "linux" {
		// keyboard/mouse sharing injects through /dev/uinput
		f, err := os.OpenFile("/dev/uinput", os.O_WRONLY, 0)
		if f != nil {
			f.Close()
		}
		report(err == nil, "/dev/uinput writable (keyboard/mouse sharing)",
			fmt.Sprintf("/dev/uinput not writable (%v): keyboard/mouse sharing will not work. Run install.sh, or log out and in once after it", err))
		br := ""
		for _, b := range []string{"google-chrome", "chromium", "chromium-browser", "microsoft-edge", "brave-browser"} {
			if _, err := exec.LookPath(b); err == nil {
				br = b
				break
			}
		}
		report(br != "", "browser for the app window: "+br, "no Chromium-family browser: the app window opens in the default browser instead")
	}
	if bad {
		os.Exit(1)
	}
}
