//go:build windows

package main

import (
	"math"
	"unsafe"
)

var (
	pSendInput       = user32.NewProc("SendInput")
	pGetSystemMetric = user32.NewProc("GetSystemMetrics")
)

type mouseInput struct {
	typ    uint32
	_      uint32
	dx, dy int32
	data   uint32
	flags  uint32
	time   uint32
	_      uint32
	extra  uintptr
}

type keyInput struct {
	typ   uint32
	_     uint32
	vk    uint16
	scan  uint16
	flags uint32
	time  uint32
	_     uint32
	extra uintptr
	_     [8]byte
}

func metric(i int) int { r, _, _ := pGetSystemMetric.Call(uintptr(i)); return int(int32(r)) }

func injectEvents(m monitor, evs []inEv) {
	vx, vy, vw, vh := metric(76), metric(77), metric(78), metric(79)
	if vw < 2 || vh < 2 {
		return
	}
	mouse := func(flags uint32, data int32, x, y float64) {
		in := mouseInput{dx: 0, dy: 0, data: uint32(data), flags: flags}
		if flags&1 != 0 {
			px := m.X + int(math.Max(0, math.Min(1, x))*float64(m.W-1))
			py := m.Y + int(math.Max(0, math.Min(1, y))*float64(m.H-1))
			in.dx = int32((px - vx) * 65535 / (vw - 1))
			in.dy = int32((py - vy) * 65535 / (vh - 1))
			in.flags |= 0x8000 | 0x4000 // absolute, virtual desktop
		}
		pSendInput.Call(1, uintptr(unsafe.Pointer(&in)), unsafe.Sizeof(in))
	}
	down := map[int]uint32{0: 0x2, 1: 0x20, 2: 0x8}
	up := map[int]uint32{0: 0x4, 1: 0x40, 2: 0x10}
	for _, e := range evs {
		switch e.E {
		case "m":
			mouse(1, 0, e.X, e.Y)
		case "d":
			mouse(1, 0, e.X, e.Y)
			mouse(down[e.B], 0, 0, 0)
		case "u":
			mouse(1, 0, e.X, e.Y)
			mouse(up[e.B], 0, 0, 0)
		case "w":
			mouse(0x800, int32(e.D), 0, 0)
		case "kd", "ku":
			if e.K <= 0 || e.K > 254 {
				continue
			}
			in := keyInput{typ: 1, vk: uint16(e.K)}
			if e.E == "ku" {
				in.flags |= 2
			}
			if e.B == 1 {
				in.flags |= 1
			}
			pSendInput.Call(1, uintptr(unsafe.Pointer(&in)), unsafe.Sizeof(in))
		}
	}
}
