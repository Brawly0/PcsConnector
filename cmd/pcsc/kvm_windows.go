//go:build windows

package main

import (
	"log"
	"runtime"
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
	a       *app
	side    int // 0 = laptop is to the right of the PC, 1 = left
	lw, lh  float64
	remote  bool
	rx, ry  float64
	cx, cy  int32
	vx, vy  int
	vw, vh  int
	out     chan inEv
	tid     uint32
	stopped atomic.Bool
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

func startKVMHost(a *app, lw, lh, side int) func() {
	h := &kvmHost{a: a, side: side, lw: float64(lw), lh: float64(lh), out: make(chan inEv, 512)}
	h.vx, h.vy, h.vw, h.vh = metric(76), metric(77), metric(78), metric(79)
	h.cx, h.cy = int32(h.vx+h.vw/2), int32(h.vy+h.vh/2)
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
		log.Print("Keyboard & mouse sharing is on: push the mouse off the screen edge to control the laptop (Scroll Lock switches)")
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
	}
}

func (h *kvmHost) sender() {
	for ev := range h.out {
		batch := []inEv{ev}
	more:
		for len(batch) < 64 {
			select {
			case e, ok := <-h.out:
				if !ok {
					break more
				}
				if e.E == "m" && batch[len(batch)-1].E == "m" {
					batch[len(batch)-1] = e // coalesce moves
				} else {
					batch = append(batch, e)
				}
			default:
				break more
			}
		}
		if s := h.a.current(); s != nil {
			s.send(msg{T: "kvm", Ev: batch})
		}
	}
}

func (h *kvmHost) emit(e inEv) {
	if h.stopped.Load() {
		return
	}
	defer func() { recover() }() // the channel may be closed during shutdown
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

// enter hands control to the laptop, entering at the edge facing the PC.
func (h *kvmHost) enter(yFrac float64) {
	h.remote = true
	h.a.kvm.active.Store(true)
	if h.side == 0 {
		h.rx = 0
	} else {
		h.rx = h.lw - 1
	}
	h.ry = clamp(yFrac*h.lh, 0, h.lh-1)
	h.park()
	e := h.pos()
	e.E = "enter"
	h.emit(e)
}

func (h *kvmHost) leave() {
	h.remote = false
	h.a.kvm.active.Store(false)
	h.emit(inEv{E: "leave"})
	x := h.vx + h.vw - 2
	if h.side == 1 {
		x = h.vx + 1
	}
	y := h.vy + int(clamp(h.ry/h.lh, 0, 1)*float64(h.vh-1))
	pSetCursorPos.Call(uintptr(x), uintptr(y))
}

func (h *kvmHost) mouse(w uintptr, ms *msllhook) bool {
	if ms.flags&1 != 0 { // injected
		return false
	}
	if !h.remote {
		if w == 0x200 {
			x := int(ms.x)
			if (h.side == 0 && x >= h.vx+h.vw-1) || (h.side == 1 && x <= h.vx) {
				h.enter(float64(int(ms.y)-h.vy) / float64(h.vh))
				return true
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
		if (h.side == 0 && h.rx < 0) || (h.side == 1 && h.rx > h.lw-1) {
			h.leave()
			return true
		}
		h.rx, h.ry = clamp(h.rx, 0, h.lw-1), clamp(h.ry, 0, h.lh-1)
		h.emit(h.pos())
	case 0x201:
		h.emit(inEv{E: "d", B: 0})
	case 0x202:
		h.emit(inEv{E: "u", B: 0})
	case 0x204:
		h.emit(inEv{E: "d", B: 2})
	case 0x205:
		h.emit(inEv{E: "u", B: 2})
	case 0x207:
		h.emit(inEv{E: "d", B: 1})
	case 0x208:
		h.emit(inEv{E: "u", B: 1})
	case 0x20A, 0x20E:
		d := int(int16(ms.data >> 16))
		n := d / 120
		if n == 0 && d != 0 {
			n = d / abs(d)
		}
		e := "w"
		if w == 0x20E {
			e = "h"
		}
		h.emit(inEv{E: e, D: n})
	}
	return true
}

func abs(i int) int {
	if i < 0 {
		return -i
	}
	return i
}

func (h *kvmHost) key(w uintptr, kb *kbllhook) bool {
	if kb.flags&0x10 != 0 { // injected
		return false
	}
	down := w == 0x100 || w == 0x104
	if kb.vk == 145 { // Scroll Lock switches between the PC and the laptop
		if down {
			if h.remote {
				h.ry = h.lh / 2
				h.leave()
			} else {
				h.enter(0.5)
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
