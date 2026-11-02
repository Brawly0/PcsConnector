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
	"time"
)

type msg struct {
	T        string `json:"t"`
	Name     string `json:"name,omitempty"`
	Platform string `json:"platform,omitempty"`
	Text     string `json:"text,omitempty"`
	Size     int64  `json:"size,omitempty"`
}

func (s *session) send(m msg) error {
	b, _ := json.Marshal(m)
	return s.write(typJSON, b)
}

type dirs struct{ home, inbox, outbox, sent string }

type app struct {
	dirs   dirs
	key    []byte
	noClip bool

	mu  sync.Mutex
	cur *session
}

func newApp(d dirs, token string, noClip bool) (*app, error) {
	for _, p := range []string{d.inbox, d.outbox, d.sent} {
		if err := os.MkdirAll(p, 0o755); err != nil {
			return nil, err
		}
	}
	a := &app{dirs: d, key: deriveKey(token), noClip: noClip}
	debug.FreeOSMemory() // hand scrypt's 16 MB scratch space back to the OS
	go a.watchOutbox()
	log.Printf("Inbox:  %s\nOutbox: %s (drop files here to send)", d.inbox, d.outbox)
	return a, nil
}

func (a *app) current() *session {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.cur
}

// serve accepts connections; the newest one replaces the previous.
func (a *app) serve(port int) error {
	ln, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
	if err != nil {
		return err
	}
	log.Printf("hosting on port %d - waiting for the other machine...", port)
	for {
		c, err := ln.Accept()
		if err != nil {
			return err
		}
		go a.handle(newSession(c, a.key))
	}
}

// dial connects to the host and reconnects forever.
func (a *app) dial(host string, port int) {
	addr := net.JoinHostPort(host, fmt.Sprint(port))
	for {
		if c, err := net.DialTimeout("tcp", addr, 5*time.Second); err == nil {
			a.handle(newSession(c, a.key))
		}
		time.Sleep(3 * time.Second)
	}
}

func (a *app) handle(s *session) {
	a.mu.Lock()
	old := a.cur
	a.cur = s
	a.mu.Unlock()
	if old != nil {
		old.close()
	}
	defer func() {
		a.mu.Lock()
		if a.cur == s {
			a.cur = nil
		}
		a.mu.Unlock()
		s.close()
		log.Print("disconnected")
	}()

	rx := &receiver{inbox: a.dirs.inbox}
	defer rx.abort()
	var clip *clipWatcher
	if !a.noClip {
		clip = startClipboard(func(t string) { s.send(msg{T: "clip", Text: t}) })
		defer clip.stop()
	}
	if s.send(msg{T: "hello", Name: hostname(), Platform: runtime.GOOS}) != nil {
		return
	}
	for {
		typ, payload, err := s.read()
		if err != nil {
			return
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
			log.Printf("connected to %s (%s)", m.Name, m.Platform)
		case "clip":
			if clip != nil {
				clip.apply(m.Text)
			}
		case "file-start":
			rx.start(m.Name)
		case "file-end":
			rx.end()
		}
	}
}

func hostname() string { h, _ := os.Hostname(); return h }
