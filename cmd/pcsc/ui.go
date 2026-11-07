package main

// Local web UI: a small HTTP server bound to 127.0.0.1 serving one embedded page.

import (
	"crypto/rand"
	_ "embed"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"mime/multipart"
	"net"
	"net/http"
	"net/textproto"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"
)

//go:embed ui.html
var uiHTML []byte

//go:embed viewer.html
var viewerHTML []byte

const uiAddr = "127.0.0.1:24801"

type config struct {
	Token string `json:"token,omitempty"`
	Mode  string `json:"mode,omitempty"` // "host" | "join"
	Host  string `json:"host,omitempty"`
	Port  int    `json:"port,omitempty"`
}

func loadConfig(d dirs) config {
	var c config
	if b, err := os.ReadFile(filepath.Join(d.home, "config.json")); err == nil {
		json.Unmarshal(b, &c)
	}
	return c
}

func saveConfig(d dirs, c config) {
	os.MkdirAll(d.home, 0o755)
	j, _ := json.Marshal(c)
	os.WriteFile(filepath.Join(d.home, "config.json"), j, 0o600)
}

const codeAlphabet = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"

// newCode returns a readable pairing code like K7QPX-2MDRT (50 bits).
func newCode() string {
	b := make([]byte, 10)
	rand.Read(b)
	for i := range b {
		b[i] = codeAlphabet[int(b[i])%len(codeAlphabet)]
	}
	return string(b[:5]) + "-" + string(b[5:])
}

func normCode(s string) string {
	s = strings.ToUpper(strings.NewReplacer("-", "", " ", "").Replace(s))
	n := min(5, len(s))
	return s[:n] + "-" + s[n:]
}

// logRing keeps the last lines of log output for the UI.
type logRing struct {
	mu    sync.Mutex
	lines []string
}

func (l *logRing) Write(p []byte) (int, error) {
	l.mu.Lock()
	for _, ln := range strings.Split(strings.TrimRight(string(p), "\n"), "\n") {
		l.lines = append(l.lines, ln)
	}
	if len(l.lines) > 40 {
		l.lines = l.lines[len(l.lines)-40:]
	}
	l.mu.Unlock()
	return os.Stderr.Write(p)
}

func (l *logRing) get() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]string{}, l.lines...)
}

type ui struct {
	d    dirs
	port int
	ring *logRing

	mu  sync.Mutex
	a   *app
	cfg config
}

func (u *ui) start(cfg config) error {
	u.stop()
	a, err := newApp(u.d, cfg.Token, false)
	if err != nil {
		return err
	}
	u.mu.Lock()
	u.a, u.cfg = a, cfg
	u.mu.Unlock()
	saveConfig(u.d, cfg)
	if cfg.Mode == "host" {
		go func() {
			if err := a.serve(u.port); err != nil {
				log.Printf("Cannot listen on port %d: %v", u.port, err)
			}
		}()
	} else {
		go a.dial(cfg.Host, u.port)
	}
	return nil
}

func (u *ui) stop() {
	u.mu.Lock()
	a := u.a
	u.a = nil
	u.mu.Unlock()
	if a != nil {
		a.close()
	}
}

func localIPs() []string {
	var out []string
	addrs, _ := net.InterfaceAddrs()
	for _, ad := range addrs {
		if n, ok := ad.(*net.IPNet); ok && !n.IP.IsLoopback() && n.IP.To4() != nil && !n.IP.IsLinkLocalUnicast() {
			out = append(out, n.IP.String())
		}
	}
	return out
}

func listDir(dir string) []map[string]any {
	es, _ := os.ReadDir(dir)
	type item struct {
		name string
		size int64
		t    time.Time
	}
	var items []item
	for _, e := range es {
		if i, err := e.Info(); err == nil && i.Mode().IsRegular() {
			items = append(items, item{e.Name(), i.Size(), i.ModTime()})
		}
	}
	sort.Slice(items, func(i, j int) bool { return items[i].t.After(items[j].t) })
	out := []map[string]any{}
	for i, it := range items {
		if i == 8 {
			break
		}
		out = append(out, map[string]any{"name": it.name, "size": it.size})
	}
	return out
}

func (u *ui) state() map[string]any {
	u.mu.Lock()
	a, cfg := u.a, u.cfg
	u.mu.Unlock()
	st := map[string]any{
		"mode": cfg.Mode, "host": cfg.Host, "code": cfg.Token, "ips": localIPs(), "platform": runtime.GOOS,
		"running": a != nil, "connected": false, "port": u.port, "log": u.ring.get(),
		"vmon": vmonStatus(), "kvm": map[string]any{}, "inbox": listDir(u.d.inbox), "sent": listDir(u.d.sent), "dir": u.d.home,
	}
	if a != nil {
		peer, ip := a.peerInfo()
		st["connected"] = a.current() != nil && peer != nil
		st["clip"] = a.clipOn.Load()
		st["screen"] = a.screenOn()
		st["kvm"] = a.kvmStatus()
		if peer != nil {
			st["peer"] = map[string]string{"name": peer.Name, "platform": peer.Platform, "ip": ip}
		}
	}
	return st
}

func (u *ui) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write(uiHTML)
	})
	reply := func(w http.ResponseWriter, err error) {
		w.Header().Set("Content-Type", "application/json")
		if err != nil {
			w.WriteHeader(400)
			json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
			return
		}
		json.NewEncoder(w).Encode(map[string]bool{"ok": true})
	}
	mux.HandleFunc("/api/state", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(u.state())
	})
	mux.HandleFunc("/api/host", func(w http.ResponseWriter, r *http.Request) {
		u.mu.Lock()
		code := u.cfg.Token
		u.mu.Unlock()
		if code == "" || r.URL.Query().Get("new") == "1" {
			code = newCode()
		}
		reply(w, u.start(config{Token: code, Mode: "host"}))
	})
	mux.HandleFunc("/api/join", func(w http.ResponseWriter, r *http.Request) {
		var in struct{ IP, Code string }
		json.NewDecoder(r.Body).Decode(&in)
		ip := strings.TrimSpace(in.IP)
		if ip == "" || len(strings.ReplaceAll(in.Code, " ", "")) < 6 {
			reply(w, fmt.Errorf("enter the Windows PC's address and the pairing code"))
			return
		}
		reply(w, u.start(config{Token: normCode(in.Code), Mode: "join", Host: ip}))
	})
	mux.HandleFunc("/api/disconnect", func(w http.ResponseWriter, r *http.Request) {
		u.stop()
		u.mu.Lock()
		u.cfg = config{}
		u.mu.Unlock()
		saveConfig(u.d, config{})
		reply(w, nil)
	})
	mux.HandleFunc("/api/clip", func(w http.ResponseWriter, r *http.Request) {
		u.mu.Lock()
		a := u.a
		u.mu.Unlock()
		if a != nil {
			a.clipOn.Store(r.URL.Query().Get("on") == "1")
		}
		reply(w, nil)
	})
	mux.HandleFunc("/api/screen", func(w http.ResponseWriter, r *http.Request) {
		u.mu.Lock()
		a := u.a
		u.mu.Unlock()
		if a == nil {
			reply(w, fmt.Errorf("not running"))
			return
		}
		if r.URL.Query().Get("on") != "1" {
			a.screenEnd()
			reply(w, nil)
			return
		}
		reply(w, a.screenStart())
	})
	mux.HandleFunc("/screen", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write(viewerHTML)
	})
	mux.HandleFunc("/api/stream", func(w http.ResponseWriter, r *http.Request) {
		u.mu.Lock()
		a := u.a
		u.mu.Unlock()
		if a == nil {
			http.Error(w, "not running", 400)
			return
		}
		mw := multipart.NewWriter(w)
		w.Header().Set("Content-Type", "multipart/x-mixed-replace; boundary="+mw.Boundary())
		w.Header().Set("Cache-Control", "no-store")
		fl, _ := w.(http.Flusher)
		seq := 0
		for r.Context().Err() == nil {
			jpg, n, live := a.frame(seq, 5*time.Second)
			if !live {
				return
			}
			if jpg == nil {
				continue
			}
			seq = n
			p, err := mw.CreatePart(textproto.MIMEHeader{"Content-Type": {"image/jpeg"}, "Content-Length": {fmt.Sprint(len(jpg))}})
			if err != nil {
				return
			}
			p.Write(jpg)
			if fl != nil {
				fl.Flush()
			}
		}
	})
	mux.HandleFunc("/api/screensize", func(w http.ResponseWriter, r *http.Request) {
		u.mu.Lock()
		a := u.a
		u.mu.Unlock()
		var wd, ht int
		fmt.Sscan(r.URL.Query().Get("w"), &wd)
		fmt.Sscan(r.URL.Query().Get("h"), &ht)
		if a != nil && wd > 200 && ht > 200 {
			a.localScreen(wd, ht)
		}
		w.WriteHeader(204)
	})
	mux.HandleFunc("/api/kvm", func(w http.ResponseWriter, r *http.Request) {
		u.mu.Lock()
		a := u.a
		u.mu.Unlock()
		if a != nil {
			a.kvmOn.Store(r.URL.Query().Get("on") != "0")
			if r.URL.Query().Get("side") == "left" {
				a.kvmSide.Store(1)
			} else {
				a.kvmSide.Store(0)
			}
			a.kvmRestart()
		}
		reply(w, nil)
	})
	mux.HandleFunc("/api/input", func(w http.ResponseWriter, r *http.Request) {
		u.mu.Lock()
		a := u.a
		u.mu.Unlock()
		var evs []inEv
		if a != nil && json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&evs) == nil {
			a.sendInput(evs)
		}
		w.WriteHeader(204)
	})
	mux.HandleFunc("/api/vmon/install", func(w http.ResponseWriter, r *http.Request) {
		reply(w, installDisplay())
	})
	mux.HandleFunc("/api/send", func(w http.ResponseWriter, r *http.Request) {
		name := filepath.Base(r.URL.Query().Get("name"))
		if name == "." || name == string(filepath.Separator) || name == "" {
			reply(w, fmt.Errorf("missing file name"))
			return
		}
		// write under a temp name so the outbox watcher only sees the finished file
		tmp := filepath.Join(u.d.home, ".upload-"+name)
		f, err := os.Create(tmp)
		if err != nil {
			reply(w, err)
			return
		}
		_, err = io.Copy(f, r.Body)
		f.Close()
		if err != nil {
			os.Remove(tmp)
			reply(w, err)
			return
		}
		reply(w, os.Rename(tmp, uniquePath(u.d.outbox, name)))
	})
	mux.HandleFunc("/api/open", func(w http.ResponseWriter, r *http.Request) {
		dir := map[string]string{"inbox": u.d.inbox, "outbox": u.d.outbox}[r.URL.Query().Get("dir")]
		if dir != "" {
			switch runtime.GOOS {
			case "windows":
				exec.Command("explorer", dir).Start()
			default:
				exec.Command("xdg-open", dir).Start()
			}
		}
		reply(w, nil)
	})
	return guard(mux)
}

// guard blocks DNS-rebinding and cross-site requests against the local API.
func guard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Host != uiAddr && r.Host != "localhost:24801" {
			http.Error(w, "forbidden", 403)
			return
		}
		if r.Method == http.MethodPost && r.Header.Get("X-PCSC") != "1" {
			http.Error(w, "forbidden", 403)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func runUI(d dirs, port int, open bool) {
	u := &ui{d: d, port: port, ring: &logRing{}}
	log.SetOutput(u.ring)
	ln, err := net.Listen("tcp", uiAddr)
	if err != nil {
		// already running: just show the existing window
		if open {
			openApp("http://" + uiAddr)
		}
		return
	}
	if cfg := loadConfig(d); cfg.Mode != "" && cfg.Token != "" {
		if cfg.Mode == "host" || cfg.Host != "" {
			u.start(cfg)
		}
	}
	if open {
		openApp("http://" + uiAddr)
	}
	log.Printf("PcsConnector is open at http://%s", uiAddr)
	log.Fatal(http.Serve(ln, u.handler()))
}
