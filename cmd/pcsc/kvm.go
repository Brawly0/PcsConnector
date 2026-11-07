package main

// Input-Leap-style sharing: push the Windows mouse off a screen edge and it controls the laptop's own
// desktop; the keyboard follows. The Windows side captures input with low-level hooks, the laptop side
// injects it through uinput. Events travel as msg{T:"kvm"} with positions as 0..1 fractions of the laptop screen.

import (
	"runtime"
	"sync"
	"sync/atomic"
)

type kvmState struct {
	mu     sync.Mutex
	stop   func()
	active atomic.Bool // input currently goes to the laptop
	pw, ph int         // host: laptop screen size
	lw, lh int         // laptop: own screen size
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

// kvmRestart (host) starts the hooks when the laptop is connected, has reported its size and sharing is on.
func (a *app) kvmRestart() {
	a.kvmHalt()
	if runtime.GOOS != "windows" || !a.kvmOn.Load() || a.current() == nil {
		return
	}
	a.kvm.mu.Lock()
	w, h := a.kvm.pw, a.kvm.ph
	a.kvm.mu.Unlock()
	if w <= 0 || h <= 0 {
		return
	}
	stop := startKVMHost(a, w, h, int(a.kvmSide.Load()))
	a.kvm.mu.Lock()
	a.kvm.stop = stop
	a.kvm.mu.Unlock()
}

func (a *app) kvmPeerSize(w, h int) {
	a.kvm.mu.Lock()
	a.kvm.pw, a.kvm.ph = w, h
	a.kvm.mu.Unlock()
	a.kvmRestart()
}

// localScreen (laptop) records this screen's size and tells the host.
func (a *app) localScreen(w, h int) {
	a.kvm.mu.Lock()
	a.kvm.lw, a.kvm.lh = w, h
	a.kvm.mu.Unlock()
	a.sendScreenSize()
}

func (a *app) sendScreenSize() {
	a.kvm.mu.Lock()
	w, h := a.kvm.lw, a.kvm.lh
	a.kvm.mu.Unlock()
	if s := a.current(); s != nil && w > 0 && runtime.GOOS != "windows" {
		s.send(msg{T: "screen-size", W: w, H: h})
	}
}

func (a *app) kvmStatus() map[string]any {
	a.kvm.mu.Lock()
	ready := a.kvm.pw > 0
	a.kvm.mu.Unlock()
	return map[string]any{"on": a.kvmOn.Load(), "side": a.kvmSide.Load(), "active": a.kvm.active.Load(), "ready": ready}
}
