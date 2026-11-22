package main

// Screen layout for mouse sharing: the laptop's screen is a rectangle placed in the PC's desktop
// coordinates, next to any edge of any PC monitor (like Windows' display settings). The mouse crosses
// wherever the laptop rectangle touches a PC monitor, and only there.

import (
	"encoding/json"
	"math"
	"os"
	"path/filepath"
)

type rectI struct{ X, Y, W, H int }

func (r rectI) has(x, y int) bool { return x >= r.X && x < r.X+r.W && y >= r.Y && y < r.Y+r.H }

func (r rectI) overlaps(o rectI) bool {
	return r.X < o.X+o.W && o.X < r.X+r.W && r.Y < o.Y+o.H && o.Y < r.Y+r.H
}

// geo is everything the host needs to decide crossings.
type geo struct {
	mons []rectI // PC monitors
	L    rectI   // laptop screen, in PC desktop coordinates (its own pixel size)
}

func (g *geo) onPC(x, y int) bool {
	for _, m := range g.mons {
		if m.has(x, y) {
			return true
		}
	}
	return false
}

// enterAt: the PC cursor is at (x, y), as the mouse hook reports it (on the edge pixel, or already past
// it). If the laptop is right there, it returns the entry point in laptop pixels.
func (g *geo) enterAt(x, y int) (lx, ly float64, ok bool) {
	for _, d := range [5][2]int{{0, 0}, {1, 0}, {-1, 0}, {0, 1}, {0, -1}} {
		qx, qy := x+d[0], y+d[1]
		if !g.onPC(qx, qy) && g.L.has(qx, qy) {
			return float64(qx - g.L.X), float64(qy - g.L.Y), true
		}
	}
	return 0, 0, false
}

// leaveAt: the laptop cursor moved to (rx, ry) in laptop pixels, possibly off the laptop screen. If that
// spot is on a PC monitor it returns where to put the PC cursor, one pixel in from the shared edge so
// the next small move back doesn't bounce straight onto the laptop again.
func (g *geo) leaveAt(rx, ry float64) (x, y int, ok bool) {
	if rx >= 0 && ry >= 0 && rx < float64(g.L.W) && ry < float64(g.L.H) {
		return 0, 0, false
	}
	x, y = g.L.X+int(math.Floor(rx)), g.L.Y+int(math.Floor(ry))
	if !g.onPC(x, y) {
		return 0, 0, false
	}
	sx, sy := 0, 0
	switch {
	case rx < 0:
		sx = -1
	case rx >= float64(g.L.W):
		sx = 1
	}
	switch {
	case ry < 0:
		sy = -1
	case ry >= float64(g.L.H):
		sy = 1
	}
	if g.onPC(x+sx, y+sy) {
		x, y = x+sx, y+sy
	}
	return x, y, true
}

// minTouch is how much of the laptop edge must touch a PC monitor (pixels), so there is always a
// usable stretch to cross through.
const minTouch = 120

// snapLayout moves the laptop rectangle (w x h, wanted top-left at x, y) to the nearest spot that
// touches a PC monitor edge without overlapping any monitor. Edges within `pull` pixels of lining up
// with that monitor's edges or centre snap into line.
func snapLayout(mons []rectI, w, h, x, y, pull int) (int, int) {
	if len(mons) == 0 || w <= 0 || h <= 0 {
		return x, y
	}
	align := func(v, size, lo, length int) int {
		t := min(minTouch, size, length)
		v = max(lo-size+t, min(v, lo+length-t)) // keep at least t pixels of touch
		for _, s := range []int{lo, lo + length - size, lo + (length-size)/2} {
			if abs(v-s) <= pull {
				return s
			}
		}
		return v
	}
	best, bx, by := -1, x, y
	for _, m := range mons {
		ay, ax := align(y, h, m.Y, m.H), align(x, w, m.X, m.W)
		for _, c := range [4][2]int{{m.X + m.W, ay}, {m.X - w, ay}, {ax, m.Y - h}, {ax, m.Y + m.H}} {
			r := rectI{c[0], c[1], w, h}
			clash := false
			for _, o := range mons {
				if r.overlaps(o) {
					clash = true
					break
				}
			}
			if clash {
				continue
			}
			if d := (c[0]-x)*(c[0]-x) + (c[1]-y)*(c[1]-y); best < 0 || d < best {
				best, bx, by = d, c[0], c[1]
			}
		}
	}
	return bx, by
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

// defaultLayout puts the laptop next to the main monitor (mons[0]): to its right if that is free, else
// left, above or below; centred on that edge. Failing all four, the spot nearest its right edge.
func defaultLayout(mons []rectI, w, h int) (int, int) {
	if len(mons) == 0 {
		return 0, 0
	}
	r := mons[0]
	cx, cy := r.X+(r.W-w)/2, r.Y+(r.H-h)/2
	for _, c := range [4][2]int{{r.X + r.W, cy}, {r.X - w, cy}, {cx, r.Y - h}, {cx, r.Y + r.H}} {
		x, y := snapLayout(mons, w, h, c[0], c[1], 0)
		if touches(rectI{x, y, w, h}, r) {
			return x, y
		}
	}
	return snapLayout(mons, w, h, r.X+r.W, cy, 0)
}

// touches: a and b share an edge for at least one pixel.
func touches(a, b rectI) bool {
	ov := func(a0, a1, b0, b1 int) bool { return max(a0, b0) < min(a1, b1) }
	return ((a.X == b.X+b.W || a.X+a.W == b.X) && ov(a.Y, a.Y+a.H, b.Y, b.Y+b.H)) ||
		((a.Y == b.Y+b.H || a.Y+a.H == b.Y) && ov(a.X, a.X+a.W, b.X, b.X+b.W))
}

// savedLayout is the user's placement, kept in layout.json next to config.json.
type savedLayout struct {
	X, Y int
	Set  bool
}

func loadLayout(d dirs) savedLayout {
	var l savedLayout
	if b, err := os.ReadFile(filepath.Join(d.home, "layout.json")); err == nil {
		json.Unmarshal(b, &l)
	}
	return l
}

func saveLayout(d dirs, l savedLayout) {
	os.MkdirAll(d.home, 0o755)
	j, _ := json.Marshal(l)
	os.WriteFile(filepath.Join(d.home, "layout.json"), j, 0o600)
}

// layoutMonitors lists the PC monitors that count for the layout, main monitor first. The extended
// screen's virtual monitor only counts while it is being streamed: otherwise nobody can see it, and
// the mouse should go to the laptop instead of wandering onto it.
func layoutMonitors(withVirtual bool) []monitor {
	var out []monitor
	for _, m := range listMonitors() {
		if m.Virtual && !withVirtual {
			continue
		}
		if m.Primary {
			out = append([]monitor{m}, out...)
		} else {
			out = append(out, m)
		}
	}
	return out
}

func rects(ms []monitor) []rectI {
	out := make([]rectI, 0, len(ms))
	for _, m := range ms {
		out = append(out, rectI{m.X, m.Y, m.W, m.H})
	}
	return out
}

// makeGeo builds the crossing geometry from the current monitors, the laptop size and the saved layout.
func makeGeo(mons []rectI, w, h int, l savedLayout) geo {
	var x, y int
	if l.Set {
		x, y = snapLayout(mons, w, h, l.X, l.Y, 0)
	} else {
		x, y = defaultLayout(mons, w, h)
	}
	return geo{mons: mons, L: rectI{x, y, w, h}}
}
