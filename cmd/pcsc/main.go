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
  pcsc join <ip> [--port N]   run on the Ubuntu laptop
  pcsc doctor                 check required tools
options: --token T  --no-clipboard      env: PCSC_TOKEN, PCSC_HOME`

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
	need := func() {
		if tok == "" {
			log.Fatal("No token. Run `pcsc token` on one machine, then pass the same value with --token on the other.")
		}
	}

	switch cmd {
	case "ui":
		runUI(d, *port, true)
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
		if positional == "" {
			log.Fatal("Usage: pcsc join <host-ip> [--token T]")
		}
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
	need := []string{"ffplay"}
	if runtime.GOOS == "windows" {
		need = []string{"ffmpeg"}
	} else if os.Getenv("WAYLAND_DISPLAY") != "" {
		need = append(need, "wl-paste", "wl-copy")
	} else {
		need = append(need, "xclip")
	}
	fmt.Printf("platform: %s/%s\n", runtime.GOOS, runtime.GOARCH)
	for _, c := range need {
		if _, err := exec.LookPath(c); err != nil {
			fmt.Printf("MISSING %s\n", c)
		} else {
			fmt.Printf("ok      %s\n", c)
		}
	}
}
