//go:build linux

package main

// Laptop side: inject the PC's mouse and keyboard through uinput (works on X11 and Wayland).

import (
	"encoding/binary"
	"log"
	"os"
	"sync"
	"syscall"
	"time"
	"unsafe"
)

const absMax = 32767

type uinputDev struct {
	mu       sync.Mutex
	kb, ms   *os.File
	heldKeys map[int]bool
	heldBtns map[int]bool
	failed   bool
}

var uin uinputDev

func ioctl(f *os.File, req, arg uintptr) error {
	if _, _, e := syscall.Syscall(syscall.SYS_IOCTL, f.Fd(), req, arg); e != 0 {
		return e
	}
	return nil
}

func uiSet(f *os.File, req uintptr, v int) { ioctl(f, req, uintptr(v)) }

func uiCreate(name string, setup func(f *os.File)) (*os.File, error) {
	f, err := os.OpenFile("/dev/uinput", os.O_WRONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, err
	}
	setup(f)
	var us [92]byte
	binary.LittleEndian.PutUint16(us[0:], 3) // BUS_USB
	binary.LittleEndian.PutUint16(us[2:], 0x1209)
	binary.LittleEndian.PutUint16(us[4:], 0x7c5c)
	copy(us[8:], name)
	if err := ioctl(f, 0x405c5503, uintptr(unsafe.Pointer(&us[0]))); err != nil { // UI_DEV_SETUP
		f.Close()
		return nil, err
	}
	if err := ioctl(f, 0x5501, 0); err != nil { // UI_DEV_CREATE
		f.Close()
		return nil, err
	}
	return f, nil
}

func (u *uinputDev) init() bool {
	if u.kb != nil {
		return true
	}
	if u.failed {
		return false
	}
	kb, err := uiCreate("PcsConnector keyboard", func(f *os.File) {
		uiSet(f, 0x40045564, 1) // EV_KEY
		for c := 1; c < 249; c++ {
			uiSet(f, 0x40045565, c)
		}
	})
	if err == nil {
		var ms *os.File
		ms, err = uiCreate("PcsConnector mouse", func(f *os.File) {
			uiSet(f, 0x40045564, 1) // EV_KEY
			uiSet(f, 0x40045564, 2) // EV_REL
			uiSet(f, 0x40045564, 3) // EV_ABS
			for _, c := range []int{272, 273, 274} {
				uiSet(f, 0x40045565, c)
			}
			uiSet(f, 0x40045566, 8) // REL_WHEEL
			uiSet(f, 0x40045566, 6) // REL_HWHEEL
			for _, ax := range []int{0, 1} {
				uiSet(f, 0x40045567, ax)
				var as [28]byte
				binary.LittleEndian.PutUint16(as[0:], uint16(ax))
				binary.LittleEndian.PutUint32(as[12:], absMax) // maximum
				ioctl(f, 0x401c5504, uintptr(unsafe.Pointer(&as[0])))
			}
		})
		if err == nil {
			u.kb, u.ms = kb, ms
			u.heldKeys, u.heldBtns = map[int]bool{}, map[int]bool{}
			time.Sleep(150 * time.Millisecond) // let the desktop notice the new devices
			return true
		}
		kb.Close()
	}
	u.failed = true
	log.Printf("Cannot share keyboard/mouse: %v. Re-run install.sh (it sets up /dev/uinput access), then log out and in.", err)
	return false
}

func uiEvent(f *os.File, typ, code uint16, val int32) {
	var b [24]byte
	binary.LittleEndian.PutUint16(b[16:], typ)
	binary.LittleEndian.PutUint16(b[18:], code)
	binary.LittleEndian.PutUint32(b[20:], uint32(val))
	f.Write(b[:])
}

func (u *uinputDev) releaseAll() {
	if u.kb == nil {
		return
	}
	for c := range u.heldKeys {
		uiEvent(u.kb, 1, uint16(c), 0)
	}
	for c := range u.heldBtns {
		uiEvent(u.ms, 1, uint16(c), 0)
	}
	uiEvent(u.kb, 0, 0, 0)
	uiEvent(u.ms, 0, 0, 0)
	u.heldKeys, u.heldBtns = map[int]bool{}, map[int]bool{}
}

var vkToEvdev = func() map[int]int {
	m := map[int]int{8: 14, 9: 15, 13: 28, 16: 42, 17: 29, 18: 56, 19: 119, 20: 58, 27: 1, 32: 57, 33: 104, 34: 109, 35: 107, 36: 102,
		37: 105, 38: 103, 39: 106, 40: 108, 45: 110, 46: 111, 91: 125, 92: 126, 93: 127, 106: 55, 107: 78, 109: 74, 110: 83, 111: 98,
		144: 69, 145: 70, 160: 42, 161: 54, 162: 29, 163: 97, 164: 56, 165: 100,
		186: 39, 187: 13, 188: 51, 189: 12, 190: 52, 191: 53, 192: 41, 219: 26, 220: 43, 221: 27, 222: 40}
	for c, e := range map[byte]int{'A': 30, 'B': 48, 'C': 46, 'D': 32, 'E': 18, 'F': 33, 'G': 34, 'H': 35, 'I': 23, 'J': 36, 'K': 37, 'L': 38, 'M': 50,
		'N': 49, 'O': 24, 'P': 25, 'Q': 16, 'R': 19, 'S': 31, 'T': 20, 'U': 22, 'V': 47, 'W': 17, 'X': 45, 'Y': 21, 'Z': 44} {
		m[int(c)] = e
	}
	for i, e := range []int{11, 2, 3, 4, 5, 6, 7, 8, 9, 10} {
		m[48+i] = e
	}
	for i, e := range []int{82, 79, 80, 81, 75, 76, 77, 71, 72, 73} {
		m[96+i] = e
	}
	for i := 0; i < 10; i++ {
		m[112+i] = 59 + i // F1-F10
	}
	m[122], m[123] = 87, 88 // F11, F12
	return m
}()

func clamp01(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

func (a *app) kvmRecv(evs []inEv) {
	u := &uin
	u.mu.Lock()
	defer u.mu.Unlock()
	if !u.init() {
		return
	}
	btn := map[int]uint16{0: 272, 1: 274, 2: 273}
	for _, e := range evs {
		switch e.E {
		case "enter", "m":
			uiEvent(u.ms, 3, 0, int32(clamp01(e.X)*absMax))
			uiEvent(u.ms, 3, 1, int32(clamp01(e.Y)*absMax))
			uiEvent(u.ms, 0, 0, 0)
		case "d", "u":
			c := btn[e.B]
			v := int32(0)
			if e.E == "d" {
				v = 1
				u.heldBtns[int(c)] = true
			} else {
				delete(u.heldBtns, int(c))
			}
			uiEvent(u.ms, 1, c, v)
			uiEvent(u.ms, 0, 0, 0)
		case "w", "h":
			code := uint16(8)
			if e.E == "h" {
				code = 6
			}
			uiEvent(u.ms, 2, code, int32(e.D))
			uiEvent(u.ms, 0, 0, 0)
		case "kd", "ku":
			code := vkToEvdev[e.K]
			if e.K == 13 && e.B == 1 {
				code = 96 // keypad enter
			}
			if code == 0 {
				continue
			}
			if e.E == "kd" {
				if u.heldKeys[code] {
					continue // the desktop generates key repeat itself
				}
				u.heldKeys[code] = true
				uiEvent(u.kb, 1, uint16(code), 1)
			} else {
				delete(u.heldKeys, code)
				uiEvent(u.kb, 1, uint16(code), 0)
			}
			uiEvent(u.kb, 0, 0, 0)
		case "leave":
			u.releaseAll()
		}
	}
}

func (a *app) kvmRelease() {
	uin.mu.Lock()
	uin.releaseAll()
	uin.mu.Unlock()
}
