package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net"
	"os"
	"runtime"
	"runtime/debug"
	"sync"
	"sync/atomic"
	"time"
)

type msg struct {
	T        string `json:"t"`
	Name     string `json:"name,omitempty"`
	Platform string `json:"platform,omitempty"`
	Text     string `json:"text,omitempty"`
	Size     int64  `json:"size,omitempty"`
	W        int    `json:"w,omitempty"`
	H        int    `json:"h,omitempty"`
	Ev       []inEv `json:"ev,omitempty"`
}

func (s *session) send(m msg) error {
	b, _ := json.Marshal(m)
	return s.write(typJSON, b)
}

type dirs struct{ home, inbox, outbox, sent string }

type app struct {
	dirs dirs
	key  []byte

	clipOn atomic.Bool
	done   chan struct{}
	once   sync.Once

	mu     sync.Mutex
	ln     net.Listener
	cur    *session
	peer   *msg   // set once the peer's hello arrives
	peerIP string // remote address of the current connection

	scr screenState

	kvm     kvmState
	kvmOn   atomic.Bool
	kvmSide atomic.Int32
}

func newApp(d dirs, token string, noClip bool) (*app, error) {
	for _, p := range []string{d.inbox, d.outbox, d.sent} {
		if err := os.MkdirAll(p, 0o755); err != nil {
			return nil, err
		}
	}
	a := &app{dirs: d, key: deriveKey(token), done: make(chan struct{})}
	a.clipOn.Store(!noClip)
	a.kvmOn.Store(true)
	debug.FreeOSMemory() // hand scrypt's 16 MB scratch space back to the OS
	go a.watchOutbox()
	return a, nil
}

func (a *app) stopped() bool {
	select {
	case <-a.done:
		return true
	default:
		return false
	}
}

func (a *app) close() {
	a.once.Do(func() {
		close(a.done)
		a.mu.Lock()
		ln, cur := a.ln, a.cur
		a.mu.Unlock()
		if ln != nil {
			ln.Close()
		}
		if cur != nil {
			cur.close()
		}
		a.screenStop()
	})
}

func (a *app) current() *session {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.cur
}

func (a *app) peerInfo() (*msg, string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.peer, a.peerIP
}

// serve accepts connections; the newest one replaces the previous.
func (a *app) serve(port int) error {
	ln, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
	if err != nil {
		return err
	}
	a.mu.Lock()
	a.ln = ln
	a.mu.Unlock()
	log.Printf("Waiting for the other computer on port %d", port)
	for {
		c, err := ln.Accept()
		if err != nil {
			if a.stopped() {
				return nil
			}
			return err
		}
		go a.handle(newSession(c, a.key))
	}
}

// dial connects to the host and reconnects until the app is closed.
func (a *app) dial(host string, port int) {
	addr := net.JoinHostPort(host, fmt.Sprint(port))
	log.Printf("Connecting to %s ...", addr)
	for !a.stopped() {
		if c, err := net.DialTimeout("tcp", addr, 5*time.Second); err == nil {
			a.handle(newSession(c, a.key))
		}
		select {
		case <-a.done:
		case <-time.After(3 * time.Second):
		}
	}
}

func (a *app) handle(s *session) {
	a.mu.Lock()
	old := a.cur
	a.cur = s
	a.peer = nil
	a.peerIP, _, _ = net.SplitHostPort(s.c.RemoteAddr().String())
	a.mu.Unlock()
	if old != nil {
		old.close()
	}
	defer func() {
		a.mu.Lock()
		if a.cur == s {
			a.cur, a.peer = nil, nil
		}
		a.mu.Unlock()
		s.close()
		a.screenStop()
		if !a.stopped() {
			log.Print("Disconnected")
		}
	}()

	rx := &receiver{inbox: a.dirs.inbox}
	defer rx.abort()
	clip := startClipboard(a.clipOn.Load, func(t string) { s.send(msg{T: "clip", Text: t}) })
	defer clip.stop()
	if s.send(msg{T: "hello", Name: hostname(), Platform: runtime.GOOS}) != nil {
		return
	}
	for {
		typ, payload, err := s.read()
		if err != nil {
			if err.Error() != "EOF" && !a.stopped() {
				log.Printf("Connection problem: %v", err)
			}
			return
		}
		if typ == typFrame {
			a.screenFrame(payload)
			continue
		}
		if typ == typChunk {
			rx.chunk(payload)
			continue
		}
		var m msg
		if json.Unmarshal(payload, &m) != nil {
			continue
		}
		switch m.T {
		case "hello":
			a.mu.Lock()
			a.peer = &m
			a.mu.Unlock()
			log.Printf("Connected to %s (%s)", m.Name, m.Platform)
			a.sendScreenSize()
		case "clip":
			if a.clipOn.Load() {
				clip.apply(m.Text)
			}
		case "file-start":
			rx.start(m.Name)
		case "screen-size":
			a.kvmPeerSize(m.W, m.H)
		case "kvm":
			a.kvmRecv(m.Ev)
		case "input":
			a.input(m.Ev)
		case "file-end":
			rx.end()
		default:
			a.screenMessage(s, m)
		}
	}
}

func hostname() string { h, _ := os.Hostname(); return h }
