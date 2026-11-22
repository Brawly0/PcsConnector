//go:build windows

package main

import (
	"os"
	"testing"
	"time"
	"unsafe"
)

var pGetCursorPos = user32.NewProc("GetCursorPos")

func cursorPos() (int, int) {
	var p [2]int32
	pGetCursorPos.Call(uintptr(unsafe.Pointer(&p[0])))
	return int(p[0]), int(p[1])
}

// A Windows PC that entered the code reports its whole desktop natively: no app window needed.
func TestWindowsJoinReportsScreen(t *testing.T) {
	host, _ := newApp(testDirs(t), "tok-screen", true)
	host.kvmOn.Store(false) // don't hook this test machine's real mouse
	join, _ := newApp(testDirs(t), "tok-screen", true)
	go host.serve(24895)
	defer host.close()
	time.Sleep(200 * time.Millisecond)
	go join.dial("127.0.0.1", 24895)
	defer join.close()
	w, h := nativeScreen()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		host.kvm.mu.Lock()
		pw, ph := host.kvm.pw, host.kvm.ph
		host.kvm.mu.Unlock()
		if pw == w && ph == h && w > 0 {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("host never learned the joiner's screen (%dx%d)", w, h)
}

// The host only shares its mouse; it must not report a screen size itself.
func TestHostDoesNotReportScreen(t *testing.T) {
	host, _ := newApp(testDirs(t), "tok-screen2", true)
	host.kvmOn.Store(false)
	join, _ := newApp(testDirs(t), "tok-screen2", true)
	join.kvmOn.Store(false)
	go host.serve(24894)
	defer host.close()
	time.Sleep(200 * time.Millisecond)
	go join.dial("127.0.0.1", 24894)
	defer join.close()
	time.Sleep(1500 * time.Millisecond)
	join.kvm.mu.Lock()
	defer join.kvm.mu.Unlock()
	if join.kvm.pw != 0 {
		t.Fatalf("the joiner was sent a screen size: %d", join.kvm.pw)
	}
}

// TestInjectLive moves this PC's real cursor through the receiving code and checks where it lands.
// Only with PCSC_INPUT_TEST=1 (it moves your mouse for a moment, then puts it back).
func TestInjectLive(t *testing.T) {
	if os.Getenv("PCSC_INPUT_TEST") == "" {
		t.Skip("set PCSC_INPUT_TEST=1 (moves the mouse)")
	}
	ox, oy := cursorPos()
	defer pSetCursorPos.Call(uintptr(ox), uintptr(oy))
	a := &app{}
	vx, vy, vw, vh := metric(76), metric(77), metric(78), metric(79)
	wrecv.lastSeq = 0
	check := func(fx, fy float64, what string) {
		time.Sleep(60 * time.Millisecond)
		x, y := cursorPos()
		wx, wy := vx+int(fx*float64(vw-1)), vy+int(fy*float64(vh-1))
		if abs(x-wx) > 2 || abs(y-wy) > 2 {
			t.Errorf("%s: cursor at %d,%d want about %d,%d", what, x, y, wx, wy)
		}
	}
	a.kvmRecv([]inEv{{E: "enter", X: 0.5, Y: 0.5, S: 100}})
	check(0.5, 0.5, "enter")
	a.kvmRecv([]inEv{{E: "m", X: 0.25, Y: 0.75, S: 101}})
	check(0.25, 0.75, "move")
	a.kvmMove(102, 0.6, 0.4) // UDP fast path
	check(0.6, 0.4, "fast move")
	a.kvmMove(101, 0.1, 0.1) // older than the last one: ignored
	check(0.6, 0.4, "stale move")
	// a key held on the sender is held here, and released when control leaves
	a.kvmRecv([]inEv{{E: "kd", K: 0xA0, S: 103}}) // left Shift
	if r, _, _ := pGetAsyncKey.Call(0xA0); r&0x8000 == 0 {
		t.Error("shift not down")
	}
	a.kvmRecv([]inEv{{E: "leave", S: 104}})
	if r, _, _ := pGetAsyncKey.Call(0xA0); r&0x8000 != 0 {
		t.Error("shift still down after leave")
	}
	a.kvmMove(104, 0.9, 0.9) // in flight when control left: ignored
	check(0.6, 0.4, "move after leave")
}

// TestKVMOverNetworkLive: a host sends mouse events over a real connection to a Windows receiver.
func TestKVMOverNetworkLive(t *testing.T) {
	if os.Getenv("PCSC_INPUT_TEST") == "" {
		t.Skip("set PCSC_INPUT_TEST=1 (moves the mouse)")
	}
	ox, oy := cursorPos()
	defer pSetCursorPos.Call(uintptr(ox), uintptr(oy))
	host, _ := newApp(testDirs(t), "tok-kvm", true)
	host.kvmOn.Store(false)
	join, _ := newApp(testDirs(t), "tok-kvm", true)
	go host.serve(24893)
	defer host.close()
	time.Sleep(200 * time.Millisecond)
	go join.dial("127.0.0.1", 24893)
	defer join.close()
	for i := 0; i < 100 && host.current() == nil; i++ {
		time.Sleep(50 * time.Millisecond)
	}
	s := host.current()
	if s == nil {
		t.Fatal("no connection")
	}
	time.Sleep(300 * time.Millisecond)
	seq := uint64(time.Now().UnixNano())
	s.send(msg{T: "kvm", Ev: []inEv{{E: "enter", X: 0.3, Y: 0.6, S: seq}}})
	time.Sleep(200 * time.Millisecond)
	vx, vy, vw, vh := metric(76), metric(77), metric(78), metric(79)
	x, y := cursorPos()
	wx, wy := vx+int(0.3*float64(vw-1)), vy+int(0.6*float64(vh-1))
	s.send(msg{T: "kvm", Ev: []inEv{{E: "leave", S: seq + 1}}})
	if abs(x-wx) > 2 || abs(y-wy) > 2 {
		t.Fatalf("cursor at %d,%d want about %d,%d", x, y, wx, wy)
	}
}
