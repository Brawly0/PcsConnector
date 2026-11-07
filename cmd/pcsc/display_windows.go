//go:build windows

package main

// Windows display helpers: find the virtual monitor and capture a region of the desktop with GDI.

import (
	"strings"
	"syscall"
	"unsafe"
)

var (
	gdi32                  = syscall.NewLazyDLL("gdi32.dll")
	pCreateCompatibleDC    = gdi32.NewProc("CreateCompatibleDC")
	pCreateDIBSection      = gdi32.NewProc("CreateDIBSection")
	pSelectObject          = gdi32.NewProc("SelectObject")
	pBitBlt                = gdi32.NewProc("BitBlt")
	pDeleteObject          = gdi32.NewProc("DeleteObject")
	pDeleteDC              = gdi32.NewProc("DeleteDC")
	pGetDC                 = user32.NewProc("GetDC")
	pReleaseDC             = user32.NewProc("ReleaseDC")
	pEnumDisplayMonitors   = user32.NewProc("EnumDisplayMonitors")
	pGetMonitorInfoW       = user32.NewProc("GetMonitorInfoW")
	pEnumDisplayDevicesW   = user32.NewProc("EnumDisplayDevicesW")
	pSetProcessDPIAware    = user32.NewProc("SetProcessDPIAware")
	pSetProcessDpiAwareCtx = user32.NewProc("SetProcessDpiAwarenessContext")
)

func init() {
	// physical pixels, not scaled ones; per-monitor v2 if available
	if pSetProcessDpiAwareCtx.Find() == nil {
		if r, _, _ := pSetProcessDpiAwareCtx.Call(^uintptr(3)); r != 0 { // DPI_AWARENESS_CONTEXT_PER_MONITOR_AWARE_V2 = -4
			return
		}
	}
	pSetProcessDPIAware.Call()
}

type rect struct{ l, t, r, b int32 }

type monitorInfoEx struct {
	size    uint32
	monitor rect
	work    rect
	flags   uint32
	device  [32]uint16
}

type displayDevice struct {
	cb    uint32
	name  [32]uint16
	str   [128]uint16
	flags uint32
	id    [128]uint16
	key   [128]uint16
}

type monitor struct {
	X, Y, W, H int
	Primary    bool
	Virtual    bool
	Name       string
}

func listMonitors() []monitor {
	var out []monitor
	cb := syscall.NewCallback(func(h, dc, rc, lp uintptr) uintptr {
		mi := monitorInfoEx{size: uint32(unsafe.Sizeof(monitorInfoEx{}))}
		if r, _, _ := pGetMonitorInfoW.Call(h, uintptr(unsafe.Pointer(&mi))); r == 0 {
			return 1
		}
		m := monitor{X: int(mi.monitor.l), Y: int(mi.monitor.t), W: int(mi.monitor.r - mi.monitor.l), H: int(mi.monitor.b - mi.monitor.t),
			Primary: mi.flags&1 != 0, Name: syscall.UTF16ToString(mi.device[:])}
		dd := displayDevice{cb: uint32(unsafe.Sizeof(displayDevice{}))}
		if r, _, _ := pEnumDisplayDevicesW.Call(uintptr(unsafe.Pointer(&mi.device[0])), 0, uintptr(unsafe.Pointer(&dd)), 0); r != 0 {
			s := strings.ToLower(syscall.UTF16ToString(dd.str[:]) + " " + syscall.UTF16ToString(dd.id[:]))
			m.Virtual = strings.Contains(s, "virtual display") || strings.Contains(s, "mttvdd") || strings.Contains(s, "mtt1337") || strings.Contains(s, "vdd by mtt")
		}
		out = append(out, m)
		return 1
	})
	pEnumDisplayMonitors.Call(0, 0, cb, 0)
	return out
}

// virtualMonitor returns the monitor to extend onto: the virtual one, else the right-most non-primary one.
func virtualMonitor() (monitor, bool) {
	var best monitor
	found := false
	for _, m := range listMonitors() {
		if m.Virtual {
			return m, true
		}
		if !m.Primary && (!found || m.X > best.X) {
			best, found = m, true
		}
	}
	return best, found
}

func hasVirtualDriver() bool {
	for _, m := range listMonitors() {
		if m.Virtual {
			return true
		}
	}
	return false
}

type grabber struct {
	x, y, w, h int
	dc, mem    uintptr
	bmp, old   uintptr
	bits       unsafe.Pointer
}

type bitmapInfoHeader struct {
	size          uint32
	w, h          int32
	planes, bpp   uint16
	comp, imgSize uint32
	xppm, yppm    int32
	used, imp     uint32
}

func newGrabber(x, y, w, h int) (*grabber, error) {
	g := &grabber{x: x, y: y, w: w, h: h}
	g.dc, _, _ = pGetDC.Call(0)
	g.mem, _, _ = pCreateCompatibleDC.Call(g.dc)
	bi := bitmapInfoHeader{size: 40, w: int32(w), h: -int32(h), planes: 1, bpp: 32}
	g.bmp, _, _ = pCreateDIBSection.Call(g.dc, uintptr(unsafe.Pointer(&bi)), 0, uintptr(unsafe.Pointer(&g.bits)), 0, 0)
	if g.bmp == 0 || g.bits == nil {
		g.close()
		return nil, syscall.EINVAL
	}
	g.old, _, _ = pSelectObject.Call(g.mem, g.bmp)
	return g, nil
}

// grab copies the region into buf as BGRA (len w*h*4) and reports success.
func (g *grabber) grab(buf []byte) bool {
	const srccopy, captureblt = 0x00CC0020, 0x40000000
	r, _, _ := pBitBlt.Call(g.mem, 0, 0, uintptr(g.w), uintptr(g.h), g.dc, uintptr(g.x), uintptr(g.y), srccopy|captureblt)
	if r == 0 {
		return false
	}
	g.drawCursor()
	copy(buf, unsafe.Slice((*byte)(g.bits), g.w*g.h*4))
	return true
}

func (g *grabber) close() {
	if g.old != 0 {
		pSelectObject.Call(g.mem, g.old)
	}
	if g.bmp != 0 {
		pDeleteObject.Call(g.bmp)
	}
	if g.mem != 0 {
		pDeleteDC.Call(g.mem)
	}
	if g.dc != 0 {
		pReleaseDC.Call(0, g.dc)
	}
}

var pChangeDisplaySettingsExW = user32.NewProc("ChangeDisplaySettingsExW")

func monitorByName(name string) (monitor, bool) {
	for _, m := range listMonitors() {
		if m.Name == name {
			return m, true
		}
	}
	return monitor{}, false
}

var (
	pGetCursorInfo = user32.NewProc("GetCursorInfo")
	pGetIconInfo   = user32.NewProc("GetIconInfo")
	pDrawIconEx    = user32.NewProc("DrawIconEx")
)

type cursorInfo struct {
	size, flags uint32
	h           uintptr
	x, y        int32
}

type iconInfo struct {
	icon       uint32
	xHot, yHot uint32
	_          uint32
	mask, col  uintptr
}

// drawCursor paints the mouse pointer into the captured image (BitBlt leaves it out).
func (g *grabber) drawCursor() {
	ci := cursorInfo{size: uint32(unsafe.Sizeof(cursorInfo{}))}
	if r, _, _ := pGetCursorInfo.Call(uintptr(unsafe.Pointer(&ci))); r == 0 || ci.flags&1 == 0 {
		return
	}
	var ii iconInfo
	if r, _, _ := pGetIconInfo.Call(ci.h, uintptr(unsafe.Pointer(&ii))); r != 0 {
		if ii.mask != 0 {
			pDeleteObject.Call(ii.mask)
		}
		if ii.col != 0 {
			pDeleteObject.Call(ii.col)
		}
	}
	pDrawIconEx.Call(g.mem, uintptr(int(ci.x)-g.x-int(ii.xHot)), uintptr(int(ci.y)-g.y-int(ii.yHot)), ci.h, 0, 0, 0, 0, 3)
}

// placeMonitor sets the virtual monitor's size and puts it to the right of the real monitors,
// so nobody has to open Windows display settings. Reports whether anything changed.
func placeMonitor(m monitor, w, h int) bool {
	right, top, first := 0, 0, true
	for _, o := range listMonitors() {
		if o.Virtual {
			continue
		}
		if first || o.X+o.W > right {
			right = o.X + o.W
		}
		if o.Primary {
			top = o.Y
		}
		first = false
	}
	if m.W == w && m.H == h && m.X == right && m.Y == top {
		return false
	}
	dm := make([]byte, 220)
	*(*uint16)(unsafe.Pointer(&dm[68])) = 220
	*(*uint32)(unsafe.Pointer(&dm[72])) = 0x80000 | 0x100000 | 0x20
	*(*int32)(unsafe.Pointer(&dm[76])) = int32(right)
	*(*int32)(unsafe.Pointer(&dm[80])) = int32(top)
	*(*uint32)(unsafe.Pointer(&dm[172])) = uint32(w)
	*(*uint32)(unsafe.Pointer(&dm[176])) = uint32(h)
	name, _ := syscall.UTF16PtrFromString(m.Name)
	const updateRegistry, noReset = 1, 0x10000
	pChangeDisplaySettingsExW.Call(uintptr(unsafe.Pointer(name)), uintptr(unsafe.Pointer(&dm[0])), 0, updateRegistry|noReset, 0)
	pChangeDisplaySettingsExW.Call(0, 0, 0, 0, 0) // apply
	return true
}

func virtualCount() int {
	n := 0
	for _, m := range listMonitors() {
		if m.Virtual {
			n++
		}
	}
	return n
}
