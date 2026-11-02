package main

import (
	"log"
	"sync"
	"time"
)

const maxClip = 2 << 20 // larger clipboards are not synced

// clipWatcher reports local clipboard text changes and applies remote ones
// without echoing them back. Platform access lives in clipboard_*.go.
type clipWatcher struct {
	mu     sync.Mutex
	last   string
	primed bool // first read only records the pre-existing clipboard
	done   chan struct{}
}

func startClipboard(onChange func(string)) *clipWatcher {
	w := &clipWatcher{done: make(chan struct{})}
	_, hasSeq := clipSeq()
	interval := 700 * time.Millisecond
	if hasSeq {
		interval = 250 * time.Millisecond // cheap: only reads when the OS says it changed
	}
	go func() {
		var lastSeq uint32
		t := time.NewTicker(interval)
		defer t.Stop()
		for {
			select {
			case <-w.done:
				return
			case <-t.C:
			}
			if hasSeq {
				s, _ := clipSeq()
				if s == lastSeq && w.primed {
					continue
				}
				lastSeq = s
			}
			text, err := clipRead()
			if err != nil {
				continue
			}
			w.mu.Lock()
			changed := w.primed && text != w.last
			w.last, w.primed = text, true
			w.mu.Unlock()
			if changed && text != "" && len(text) <= maxClip {
				onChange(text)
			}
		}
	}()
	return w
}

func (w *clipWatcher) apply(text string) {
	w.mu.Lock()
	w.last = text
	w.mu.Unlock()
	if err := clipWrite(text); err != nil {
		log.Printf("clipboard write failed: %v", err)
	}
}

func (w *clipWatcher) stop() { close(w.done) }
