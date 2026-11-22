//go:build windows

package main

import (
	"log"
	"runtime"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
	"unsafe"
)

var (
	pGetThreadID   = kernel32.NewProc("GetCurrentThreadId")
	pSetHookEx     = user32.NewProc("SetWindowsHookExW")
	pUnhookEx      = user32.NewProc("UnhookWindowsHookEx")
	pCallNextHook  = user32.NewProc("CallNextHookEx")
	pGetMessage    = user32.NewProc("GetMessageW")
	pPostThreadMsg = user32.NewProc("PostThreadMessageW")
	pSetCursorPos  = user32.NewProc("SetCursorPos")
	pCreateCursor  = user32.NewProc("CreateCursor")
	pCopyIcon      = user32.NewProc("CopyIcon")
	pSetSysCursor  = user32.NewProc("SetSystemCursor")
	pSysParamInfo  = user32.NewProc("SystemParametersInfoW")
	pLoadCursor    = user32.NewProc("LoadCursorW")
	pGetAsyncKey   = user32.NewProc("GetAsyncKeyState")
	kvmCur         atomic.Pointer[kvmHost]
	mouseCB, keyCB uintptr
)

type msllhook struct {
	x, y  int32
	data  uint32
	flags uint32
	time  uint32
	extra uintptr
}

type kbllhook struct {
	vk, scan, flags, time uint32
	extra                 uintptr
}

type kvmHost struct {
	a        *app
	lw, lh   float64
	remote   bool
	rx, ry   float64
	cx, cy   int32
	out      chan inEv
	switched time.Time // last crossing; a quick crossing back is ignored so jitter at the edge cannot bounce
	held     int       // laptop-side mouse buttons currently down (bit per button)
	seq      uint64    // last event sequence number; only touched on the hook thread
	tid      uint32
	stopped  atomic.Bool
}

func init() {
	mouseCB = syscall.NewCallback(func(n int32, w, l uintptr) uintptr {
		if h := kvmCur.Load(); h != nil && n >= 0 && h.mouse(w, (*msllhook)(unsafe.Pointer(l))) {
			return 1
		}
		r, _, _ := pCallNextHook.Call(0, uintptr(n), w, l)
		return r
	})
	keyCB = syscall.NewCallback(func(n int32, w, l uintptr) uintptr {
		if h := kvmCur.Load(); h != nil && n >= 0 && h.key(w, (*kbllhook)(unsafe.Pointer(l))) {
			return 1
		}
		r, _, _ := pCallNextHook.Call(0, uintptr(n), w, l)
		return r
	})
}

func startKVMHost(a *app) func() {
	h := &kvmHost{a: a, out: make(chan inEv, 512)}
	h.seq = uint64(time.Now().UnixNano()) // keeps rising across restarts, so the laptop never sees it go back
	showPCCursor()
	reloadCursors() // in case a previous run died while the cursor was hidden
	vx, vy, vw, vh := metric(76), metric(77), metric(78), metric(79)
	h.cx, h.cy = int32(vx+vw/2), int32(vy+vh/2)
	for _, m := range listMonitors() {
		if m.Primary {
			h.cx, h.cy = int32(m.X+m.W/2), int32(m.Y+m.H/2)
		}
	}
	ready := make(chan bool, 1)
	go h.sender()
	go func() {
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		t, _, _ := pGetThreadID.Call()
		h.tid = uint32(t)
		hm, _, e1 := pSetHookEx.Call(14, mouseCB, 0, 0)
		hk, _, e2 := pSetHookEx.Call(13, keyCB, 0, 0)
		if hm == 0 || hk == 0 {
			log.Printf("Cannot capture keyboard/mouse: %v %v", e1, e2)
			ready <- false
			return
		}
		kvmCur.Store(h)
		ready <- true
		log.Print("Keyboard & mouse sharing is on: push the mouse off the screen edge to control the other computer (Scroll Lock switches)")
		var m [64]byte
		for {
			r, _, _ := pGetMessage.Call(uintptr(unsafe.Pointer(&m[0])), 0, 0, 0)
			if int32(r) <= 0 {
				break
			}
		}
		kvmCur.CompareAndSwap(h, nil)
		pUnhookEx.Call(hm)
		pUnhookEx.Call(hk)
	}()
	if !<-ready {
		h.stopped.Store(true)
		close(h.out)
		return nil
	}
	return func() {
		h.stopped.Store(true)
		pPostThreadMsg.Call(uintptr(h.tid), 0x12, 0, 0) // WM_QUIT
		close(h.out)
		showPCCursor()
	}
}

func (h *kvmHost) sender() {
	// Moves are capped at one per 4 ms (~250/s, well above the laptop's 60 Hz screen); the newest position
	// wins. A gaming mouse reports 1000/s, and sending each one only queues work on the laptop and the Wi-Fi.
	const gap = 4 * time.Millisecond
	var pending inEv
	var have bool
	var last time.Time
	t := time.NewTimer(time.Hour)
	t.Stop()
	flush := func() {
		if !have {
			return
		}
		have, last = false, time.Now()
		if !h.a.fastMove(pending) {
			if s := h.a.current(); s != nil {
				s.send(msg{T: "kvm", Ev: []inEv{pending}})
			}
		}
	}
	for {
		select {
		case e, ok := <-h.out:
			if !ok {
				return
			}
			if e.E == "m" {
				pending, have = e, true
				if d := time.Since(last); d >= gap {
					flush()
				} else {
					t.Reset(gap - d)
				}
				continue
			}
			flush() // a click lands after the move that preceded it
			if s := h.a.current(); s != nil {
				s.send(msg{T: "kvm", Ev: []inEv{e}})
			}
		case <-t.C:
			flush()
		}
	}
}

// The PC's own cursor is hidden while input goes to the laptop, so it doesn't sit in the middle of the
// PC screen. Hiding saves a copy of each system cursor and swaps in a blank one; showing puts the copies
// back. Both are quick enough to run inline on the hook thread, so the cursor never flashes at the
// parking spot and is visible the instant you come back. A couple of seconds later the user's scheme is
// reloaded from disk as well (restores animated cursors, which copies freeze).
var (
	ocrIDs   = []uintptr{32512, 32513, 32514, 32515, 32516, 32642, 32643, 32644, 32645, 32646, 32648, 32649, 32650, 32651}
	curMu    sync.Mutex
	curSaved map[uintptr]uintptr // non-nil while hidden
	curBlank uintptr
	curGen   int
)

func reloadCursors() { pSysParamInfo.Call(0x57, 0, 0, 0) } // SPI_SETCURSORS

func hidePCCursor() {
	curMu.Lock()
	defer curMu.Unlock()
	if curSaved != nil {
		return
	}
	if curBlank == 0 {
		var and, xor [128]byte // 32x32 monochrome: AND all ones + XOR all zeros = transparent
		for i := range and {
			and[i] = 0xFF
		}
		curBlank, _, _ = pCreateCursor.Call(0, 0, 0, 32, 32, uintptr(unsafe.Pointer(&and[0])), uintptr(unsafe.Pointer(&xor[0])))
		if curBlank == 0 {
			return
		}
	}
	saved := map[uintptr]uintptr{}
	for _, id := range ocrIDs {
		if c, _, _ := pLoadCursor.Call(0, id); c != 0 {
			saved[id], _, _ = pCopyIcon.Call(c)
		}
		if b, _, _ := pCopyIcon.Call(curBlank); b != 0 {
			pSetSysCursor.Call(b, id) // takes ownership of b
		}
	}
	curSaved = saved
	curGen++
}

func showPCCursor() {
	curMu.Lock()
	defer curMu.Unlock()
	if curSaved == nil {
		return
	}
	missing := false
	for _, id := range ocrIDs {
		if c := curSaved[id]; c != 0 {
			pSetSysCursor.Call(c, id)
		} else {
			missing = true
		}
	}
	curSaved = nil
	curGen++
	if missing {
		reloadCursors()
		return
	}
	gen := curGen
	go func() {
		time.Sleep(2 * time.Second)
		curMu.Lock()
		defer curMu.Unlock()
		if curSaved == nil && curGen == gen {
			reloadCursors()
		}
	}()
}

func (h *kvmHost) emit(e inEv) {
	if h.stopped.Load() {
		return
	}
	defer func() { recover() }() // the channel may be closed during shutdown
	h.seq++
	e.S = h.seq
	if e.E == "m" {
		select {
		case h.out <- e:
		default:
		}
		return
	}
	select {
	case h.out <- e:
	case <-time.After(20 * time.Millisecond):
	}
}

func (h *kvmHost) park() { pSetCursorPos.Call(uintptr(h.cx), uintptr(h.cy)) }

func clamp(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func (h *kvmHost) pos() inEv {
	return inEv{E: "m", X: h.rx / h.lw, Y: h.ry / h.lh}
}

// useGeo picks up the current layout (it can change while sharing runs). Hook thread only.
func (h *kvmHost) useGeo() *geo {
	g := h.a.kvm.geo.Load()
	if g != nil {
		h.lw, h.lh = float64(g.L.W), float64(g.L.H)
	}
	return g
}

// enter hands control to the laptop, with its cursor at (lx, ly) in laptop pixels.
func (h *kvmHost) enter(lx, ly float64) {
	h.remote = true
	h.a.kvm.active.Store(true)
	h.rx, h.ry = clamp(lx, 0, h.lw-1), clamp(ly, 0, h.lh-1)
	h.held = 0
	h.switched = time.Now()
	e := h.pos()
	e.E = "enter"
	h.emit(e)      // tell the laptop first, the local cursor work below takes a moment
	hidePCCursor() // hide before parking, so the PC cursor never flashes at the parking spot
	h.park()
}

// leave hands control back to the PC, with its cursor at (x, y).
func (h *kvmHost) leave(x, y int) {
	h.remote = false
	h.a.kvm.active.Store(false)
	h.switched = time.Now()
	h.emit(inEv{E: "leave"})
	pSetCursorPos.Call(uintptr(x), uintptr(y)) // put it at the edge first, then show it there
	showPCCursor()
}

// switchGap: after crossing, crossing straight back within this time is ignored. Hand jitter right at
// the edge otherwise flips control back and forth several times in a fraction of a second.
const switchGap = 150 * time.Millisecond

func (h *kvmHost) mouse(w uintptr, ms *msllhook) bool {
	if ms.flags&1 != 0 { // injected
		return false
	}
	if !h.remote {
		if w == 0x200 {
			if g := h.useGeo(); g != nil && time.Since(h.switched) >= switchGap {
				if lx, ly, ok := g.enterAt(int(ms.x), int(ms.y)); ok && !pcButtonDown() {
					h.enter(lx, ly)
					return true
				}
			}
		}
		return false
	}
	switch w {
	case 0x200:
		dx, dy := float64(ms.x-h.cx), float64(ms.y-h.cy)
		if dx == 0 && dy == 0 {
			return true
		}
		h.park()
		h.rx += dx
		h.ry += dy
		// no crossing back during the switch gap or while a button is held (dragging on the laptop)
		if g := h.useGeo(); g != nil && time.Since(h.switched) >= switchGap && h.held == 0 {
			if x, y, ok := g.leaveAt(h.rx, h.ry); ok {
				h.leave(x, y)
				return true
			}
		}
		h.rx, h.ry = clamp(h.rx, 0, h.lw-1), clamp(h.ry, 0, h.lh-1)
		h.emit(h.pos())
	case 0x201, 0x202, 0x204, 0x205, 0x207, 0x208:
		// buttons carry the position too: they go over TCP while moves may take the UDP fast path,
		// so the laptop places the cursor first if this is newer than the last move it applied
		e := h.pos()
		e.E = map[uintptr]string{0x201: "d", 0x202: "u", 0x204: "d", 0x205: "u", 0x207: "d", 0x208: "u"}[w]
		e.B = map[uintptr]int{0x201: 0, 0x202: 0, 0x204: 2, 0x205: 2, 0x207: 1, 0x208: 1}[w]
		if e.E == "d" {
			h.held |= 1 << e.B
		} else {
			h.held &^= 1 << e.B
		}
		h.emit(e)
	case 0x20A, 0x20E:
		// raw wheel delta (120 = one notch) so smooth-scrolling mice and touchpads scroll smoothly
		e := "wh"
		if w == 0x20E {
			e = "hh"
		}
		h.emit(inEv{E: e, D: int(int16(ms.data >> 16))})
	}
	return true
}

func (h *kvmHost) key(w uintptr, kb *kbllhook) bool {
	if kb.flags&0x10 != 0 { // injected
		return false
	}
	down := w == 0x100 || w == 0x104
	if kb.vk == 145 { // Scroll Lock switches between the PC and the laptop
		if down {
			if h.remote {
				h.leave(int(h.cx), int(h.cy))
			} else if h.useGeo() != nil {
				h.enter(h.lw/2, h.lh/2)
			}
		}
		return true
	}
	if !h.remote {
		return false
	}
	t := "ku"
	if down {
		t = "kd"
	}
	ext := 0
	if kb.flags&1 != 0 {
		ext = 1
	}
	h.emit(inEv{E: t, K: int(kb.vk), B: ext})
	return true
}

// pcButtonDown: a mouse button is held on the PC (dragging), so don't hand the mouse to the laptop.
func pcButtonDown() bool {
	for _, vk := range []uintptr{1, 2, 4} { // VK_LBUTTON, VK_RBUTTON, VK_MBUTTON
		if r, _, _ := pGetAsyncKey.Call(vk); r&0x8000 != 0 {
			return true
		}
	}
	return false
}
