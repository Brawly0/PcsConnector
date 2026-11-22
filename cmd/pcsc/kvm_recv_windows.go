//go:build windows

package main

// Receiving side of mouse/keyboard sharing on Windows (when this PC entered the other one's code):
// events are injected with SendInput. Positions are 0..1 fractions of the whole virtual desktop, which is
// the size this side reports (nativeScreen).

import (
	"math"
	"sync"
	"unsafe"
)

var pMapVirtualKey = user32.NewProc("MapVirtualKeyW")

type winRecv struct {
	mu       sync.Mutex
	lastSeq  uint64
	heldKeys map[int]int  // vk -> extended flag
	heldBtns map[int]bool // button 0/1/2
}

var wrecv = winRecv{heldKeys: map[int]int{}, heldBtns: map[int]bool{}}

// nativeScreen is this PC's whole desktop (all monitors) in physical pixels.
func nativeScreen() (int, int) { return metric(78), metric(79) }

func sendMouse(flags uint32, data int32, dx, dy int32) {
	in := mouseInput{dx: dx, dy: dy, data: uint32(data), flags: flags}
	pSendInput.Call(1, uintptr(unsafe.Pointer(&in)), unsafe.Sizeof(in))
}

// moveTo places the cursor if seq is newer than the last applied position (seq 0 = always).
func (r *winRecv) moveTo(seq uint64, x, y float64) {
	if seq != 0 && seq <= r.lastSeq {
		return
	}
	if seq != 0 {
		r.lastSeq = seq
	}
	const move, absolute, virtualDesk = 0x1, 0x8000, 0x4000
	sendMouse(move|absolute|virtualDesk, 0, int32(math.Round(clamp(x, 0, 1)*65535)), int32(math.Round(clamp(y, 0, 1)*65535)))
}

func sendKey(vk, ext int, up bool) {
	scan, _, _ := pMapVirtualKey.Call(uintptr(vk), 0) // MAPVK_VK_TO_VSC: games and some apps read scan codes
	in := keyInput{typ: 1, vk: uint16(vk), scan: uint16(scan)}
	if ext == 1 {
		in.flags |= 1 // KEYEVENTF_EXTENDEDKEY
	}
	if up {
		in.flags |= 2 // KEYEVENTF_KEYUP
	}
	pSendInput.Call(1, uintptr(unsafe.Pointer(&in)), unsafe.Sizeof(in))
}

var (
	btnDown = map[int]uint32{0: 0x2, 1: 0x20, 2: 0x8}
	btnUp   = map[int]uint32{0: 0x4, 1: 0x40, 2: 0x10}
)

func (r *winRecv) releaseAll() {
	for vk, ext := range r.heldKeys {
		sendKey(vk, ext, true)
	}
	for b := range r.heldBtns {
		sendMouse(btnUp[b], 0, 0, 0)
	}
	r.heldKeys, r.heldBtns = map[int]int{}, map[int]bool{}
}

// kvmMove applies a move that came over the UDP fast path.
func (a *app) kvmMove(seq uint64, x, y float64) {
	wrecv.mu.Lock()
	defer wrecv.mu.Unlock()
	wrecv.moveTo(seq, x, y)
}

func (a *app) kvmRecv(evs []inEv) {
	r := &wrecv
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, e := range evs {
		switch e.E {
		case "enter":
			// sequence numbers start from the sender's clock, so they only go back if that clock jumped back
			if e.S < r.lastSeq && r.lastSeq-e.S > 1<<36 {
				r.lastSeq = 0
			}
			r.moveTo(e.S, e.X, e.Y)
		case "m":
			r.moveTo(e.S, e.X, e.Y)
		case "d", "u":
			if e.B < 0 || e.B > 2 {
				continue
			}
			if e.S != 0 {
				r.moveTo(e.S, e.X, e.Y) // only if newer than the last move: click where the cursor is
			}
			if e.E == "d" {
				r.heldBtns[e.B] = true
				sendMouse(btnDown[e.B], 0, 0, 0)
			} else {
				delete(r.heldBtns, e.B)
				sendMouse(btnUp[e.B], 0, 0, 0)
			}
		case "w", "h": // older senders: whole notches
			sendMouse(wheelFlag(e.E == "h"), int32(e.D*120), 0, 0)
		case "wh", "hh":
			sendMouse(wheelFlag(e.E == "hh"), int32(e.D), 0, 0)
		case "kd", "ku":
			if e.K <= 0 || e.K > 254 {
				continue
			}
			// Windows to Windows: the codes are already Windows virtual keys. Unlike Linux, injected keys
			// don't auto-repeat, so repeats from the sender's keyboard are passed on as they come.
			if e.E == "kd" {
				r.heldKeys[e.K] = e.B
			} else {
				delete(r.heldKeys, e.K)
			}
			sendKey(e.K, e.B, e.E == "ku")
		case "leave":
			r.lastSeq = max(r.lastSeq, e.S) // drops moves still in flight on the UDP path
			r.releaseAll()
		}
	}
}

func wheelFlag(horiz bool) uint32 {
	if horiz {
		return 0x1000 // MOUSEEVENTF_HWHEEL
	}
	return 0x0800 // MOUSEEVENTF_WHEEL
}

func (a *app) kvmRelease() {
	wrecv.mu.Lock()
	wrecv.releaseAll()
	wrecv.mu.Unlock()
}
