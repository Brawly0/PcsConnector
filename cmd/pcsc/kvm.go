package main

// Input-Leap-style sharing: push the Windows mouse off a screen edge and it controls the laptop's own
// desktop; the keyboard follows. The Windows side captures input with low-level hooks, the laptop side
// injects it through uinput. Events travel as msg{T:"kvm"} with positions as 0..1 fractions of the laptop screen.

import (
	"runtime"
	"sync"
	"sync/atomic"
	"time"
)

type kvmState struct {
	mu     sync.Mutex
	stop   func()
	active atomic.Bool         // input currently goes to the laptop
	pw, ph int                 // host: laptop screen size
	lw, lh int                 // laptop: own screen size
	layout savedLayout         // host: where the user put the laptop screen
	loaded bool                // layout read from disk
	geo    atomic.Pointer[geo] // host: current crossing geometry, read by the hook thread
}

func (a *app) kvmHalt() {
	a.kvm.mu.Lock()
	st := a.kvm.stop
	a.kvm.stop = nil
	a.kvm.mu.Unlock()
	if st != nil {
		st()
	}
	a.kvm.active.Store(false)
}

// kvmRestart starts the hooks on the computer that shows the code (it shares its mouse and keyboard) once
// the other one is connected and has reported its screen size. Capturing needs Windows for now.
func (a *app) kvmRestart() {
	a.kvmHalt()
	if runtime.GOOS != "windows" || !a.serving() || !a.kvmOn.Load() || a.current() == nil {
		return
	}
	a.kvm.mu.Lock()
	w, h := a.kvm.pw, a.kvm.ph
	a.kvm.mu.Unlock()
	if w <= 0 || h <= 0 {
		return
	}
	a.kvmUpdateGeo()
	stop := startKVMHost(a)
	if stop == nil {
		return
	}
	// monitors come and go (the extended screen adds one), so refresh the geometry now and then
	quit := make(chan struct{})
	go func() {
		t := time.NewTicker(2 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-quit:
				return
			case <-t.C:
				a.kvmUpdateGeo()
			}
		}
	}()
	a.kvm.mu.Lock()
	a.kvm.stop = func() { close(quit); stop() }
	a.kvm.mu.Unlock()
}

// kvmPeerSize (host) handles the laptop's size report. The laptop repeats it every few seconds, so only
// restart when something changed: a restart drops the cursor back onto the PC mid-use.
func (a *app) kvmPeerSize(w, h int) {
	a.kvm.mu.Lock()
	same := a.kvm.pw == w && a.kvm.ph == h && a.kvm.stop != nil
	a.kvm.pw, a.kvm.ph = w, h
	a.kvm.mu.Unlock()
	if !same {
		a.kvmRestart()
	}
}

// localScreen (laptop) records this screen's size and tells the host.
func (a *app) localScreen(w, h int) {
	a.kvm.mu.Lock()
	a.kvm.lw, a.kvm.lh = w, h
	a.kvm.mu.Unlock()
	a.sendScreenSize()
}

// sendScreenSize: the computer that entered the code receives the mouse, so it tells the other one its
// screen size. Windows knows it natively; on Linux the app window reports it.
func (a *app) sendScreenSize() {
	if a.serving() {
		return
	}
	w, h := nativeScreen()
	if w <= 0 || h <= 0 {
		a.kvm.mu.Lock()
		w, h = a.kvm.lw, a.kvm.lh
		a.kvm.mu.Unlock()
	}
	if s := a.current(); s != nil && w > 0 && h > 0 {
		s.send(msg{T: "screen-size", W: w, H: h})
	}
}

func (a *app) kvmStatus() map[string]any {
	a.kvm.mu.Lock()
	ready := a.kvm.pw > 0
	a.kvm.mu.Unlock()
	return map[string]any{"on": a.kvmOn.Load(), "active": a.kvm.active.Load(), "ready": ready, "link": a.fastStatus(), "layout": a.kvmLayoutInfo(), "can": runtime.GOOS == "windows"}
}

// kvmUpdateGeo (host) recomputes the crossing geometry from the monitors, the laptop size and the layout.
func (a *app) kvmUpdateGeo() {
	mons := rects(layoutMonitors(a.screenOn()))
	a.kvm.mu.Lock()
	if !a.kvm.loaded {
		a.kvm.layout, a.kvm.loaded = loadLayout(a.dirs), true
	}
	w, h, l := a.kvm.pw, a.kvm.ph, a.kvm.layout
	a.kvm.mu.Unlock()
	if w <= 0 || h <= 0 || len(mons) == 0 {
		return
	}
	g := makeGeo(mons, w, h, l)
	if old := a.kvm.geo.Load(); old == nil || !sameGeo(old, &g) {
		a.kvm.geo.Store(&g)
	}
}

func sameGeo(a, b *geo) bool {
	if a.L != b.L || len(a.mons) != len(b.mons) {
		return false
	}
	for i := range a.mons {
		if a.mons[i] != b.mons[i] {
			return false
		}
	}
	return true
}

// kvmSetLayout (host) saves where the user dropped the laptop screen (PC desktop coordinates). It is
// snapped to the nearest monitor edge; the running capture picks it up at once.
func (a *app) kvmSetLayout(x, y int) {
	mons := rects(layoutMonitors(a.screenOn()))
	a.kvm.mu.Lock()
	w, h := a.kvm.pw, a.kvm.ph
	if w <= 0 || h <= 0 {
		w, h = 1920, 1080
	}
	x, y = snapLayout(mons, w, h, x, y, 0)
	a.kvm.layout, a.kvm.loaded = savedLayout{X: x, Y: y, Set: true}, true
	a.kvm.mu.Unlock()
	saveLayout(a.dirs, savedLayout{X: x, Y: y, Set: true})
	a.kvmUpdateGeo()
}

// kvmLayoutInfo (host) is what the UI's layout editor draws.
func (a *app) kvmLayoutInfo() map[string]any {
	mons := layoutMonitors(a.screenOn())
	if len(mons) == 0 {
		return nil
	}
	a.kvm.mu.Lock()
	if !a.kvm.loaded {
		a.kvm.layout, a.kvm.loaded = loadLayout(a.dirs), true
	}
	w, h, l := a.kvm.pw, a.kvm.ph, a.kvm.layout
	a.kvm.mu.Unlock()
	known := w > 0 && h > 0
	if !known {
		w, h = 1920, 1080 // placeholder until the laptop reports its size
	}
	g := makeGeo(rects(mons), w, h, l)
	return map[string]any{"mons": mons, "laptop": g.L, "known": known}
}
