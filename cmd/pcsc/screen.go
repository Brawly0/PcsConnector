package main

// Extended screen, built in: the Windows side captures the virtual monitor with GDI, encodes JPEG
// and streams frames over the encrypted channel; the laptop shows them fullscreen in an app window.

import (
	"bytes"
	"fmt"
	"image"
	"image/jpeg"
	"log"
	"runtime"
	"sync"
	"time"
)

type screenState struct {
	mu      sync.Mutex
	stop    chan struct{} // host: closes to stop capturing
	on      bool          // host: capturing, viewer: showing
	jpg     []byte        // viewer: latest frame
	seq     int
	wait    chan struct{} // closed and replaced whenever jpg/on changes
	w, h    int
	viewing bool
	monName string
}

func (a *app) screenOn() bool {
	a.scr.mu.Lock()
	defer a.scr.mu.Unlock()
	return a.scr.on
}

// notify must be called with mu held.
func (st *screenState) notify() {
	if st.wait != nil {
		close(st.wait)
	}
	st.wait = make(chan struct{})
}

func (a *app) screenStop() {
	st := &a.scr
	st.mu.Lock()
	if st.stop != nil {
		close(st.stop)
		st.stop = nil
	}
	st.on, st.viewing, st.jpg = false, false, nil
	st.notify()
	st.mu.Unlock()
}

// frame blocks until a frame newer than seq exists (or the stream ended / timeout).
func (a *app) frame(seq int, timeout time.Duration) (jpg []byte, next int, live bool) {
	st := &a.scr
	deadline := time.After(timeout)
	for {
		st.mu.Lock()
		if st.wait == nil {
			st.wait = make(chan struct{})
		}
		w := st.wait
		if !st.viewing {
			st.mu.Unlock()
			return nil, seq, false
		}
		if st.seq != seq && st.jpg != nil {
			j, n := st.jpg, st.seq
			st.mu.Unlock()
			return j, n, true
		}
		st.mu.Unlock()
		select {
		case <-w:
		case <-deadline:
			return nil, seq, true
		case <-a.done:
			return nil, seq, false
		}
	}
}

// screenStart is called on the Windows host.
func (a *app) screenStart() error {
	if runtime.GOOS != "windows" {
		return fmt.Errorf("the screen is shared from the Windows PC")
	}
	s := a.current()
	if s == nil {
		return fmt.Errorf("connect the laptop first")
	}
	m, ok := virtualMonitor()
	if !ok {
		return fmt.Errorf("no virtual monitor yet - press \"Set up virtual monitor\" first")
	}
	if placeMonitor(m, 1920, 1080) {
		time.Sleep(800 * time.Millisecond)
		if m2, ok := monitorByName(m.Name); ok {
			m = m2
		}
	}
	a.screenStop()
	if err := s.send(msg{T: "screen-start", W: m.W, H: m.H}); err != nil {
		return err
	}
	stop := make(chan struct{})
	a.scr.mu.Lock()
	a.scr.stop, a.scr.on, a.scr.monName = stop, true, m.Name
	a.scr.mu.Unlock()
	go a.captureLoop(s, m, stop)
	log.Printf("Extended screen started (%dx%d at %d,%d)", m.W, m.H, m.X, m.Y)
	return nil
}

func (a *app) captureLoop(s *session, m monitor, stop chan struct{}) {
	defer func() {
		a.scr.mu.Lock()
		if a.scr.stop == stop {
			a.scr.stop, a.scr.on = nil, false
		}
		a.scr.mu.Unlock()
	}()
	g, err := newGrabber(m.X, m.Y, m.W, m.H)
	if err != nil {
		log.Printf("Cannot capture the screen: %v", err)
		return
	}
	defer func() { g.close() }()
	buf, prev := make([]byte, m.W*m.H*4), make([]byte, m.W*m.H*4)
	img := image.NewRGBA(image.Rect(0, 0, m.W, m.H))
	var out bytes.Buffer
	var last, chk time.Time
	tick := time.NewTicker(40 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case <-stop:
			return
		case <-a.done:
			return
		case <-tick.C:
		}
		// follow the monitor if it was moved or resized in Windows display settings
		if time.Since(chk) > time.Second {
			chk = time.Now()
			if n, ok := monitorByName(m.Name); ok && n != m && n.W > 0 && n.H > 0 {
				if g2, err := newGrabber(n.X, n.Y, n.W, n.H); err == nil {
					g.close()
					g, m = g2, n
					buf, prev = make([]byte, m.W*m.H*4), make([]byte, m.W*m.H*4)
					img = image.NewRGBA(image.Rect(0, 0, m.W, m.H))
					s.send(msg{T: "screen-start", W: m.W, H: m.H})
				}
			}
		}
		if !g.grab(buf) {
			continue
		}
		if bytes.Equal(buf, prev) && time.Since(last) < 2*time.Second {
			continue
		}
		for i := 0; i+3 < len(buf); i += 4 {
			img.Pix[i], img.Pix[i+1], img.Pix[i+2], img.Pix[i+3] = buf[i+2], buf[i+1], buf[i], 255
		}
		out.Reset()
		if jpeg.Encode(&out, img, &jpeg.Options{Quality: 70}) != nil {
			continue
		}
		if err := s.write(typFrame, out.Bytes()); err != nil {
			return
		}
		last = time.Now()
		buf, prev = prev, buf
	}
}

func (a *app) screenEnd() {
	a.screenStop()
	if s := a.current(); s != nil {
		s.send(msg{T: "screen-stop"})
	}
}

// screenFrame stores an incoming JPEG on the viewing side.
func (a *app) screenFrame(p []byte) {
	st := &a.scr
	st.mu.Lock()
	if st.viewing {
		st.jpg = append(st.jpg[:0:0], p...)
		st.seq++
		st.notify()
	}
	st.mu.Unlock()
}

// screenMessage handles screen messages on the receiving side (the laptop).
func (a *app) screenMessage(s *session, m msg) {
	switch m.T {
	case "screen-start":
		if runtime.GOOS == "windows" {
			return
		}
		a.scr.mu.Lock()
		already := a.scr.viewing
		a.scr.viewing, a.scr.on, a.scr.w, a.scr.h = true, true, m.W, m.H
		a.scr.notify()
		a.scr.mu.Unlock()
		if !already {
			openAppFull("http://" + uiAddr + "/screen")
		}
		log.Print("Showing the Windows extended screen")
	case "screen-stop":
		a.screenStop()
	}
}
