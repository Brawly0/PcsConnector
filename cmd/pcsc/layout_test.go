package main

import "testing"

var oneMon = []rectI{{0, 0, 1920, 1080}}

func TestSnapSides(t *testing.T) {
	cases := []struct {
		name         string
		x, y, wx, wy int
	}{
		{"right", 2000, 0, 1920, 0},
		{"left", -1500, 100, -1366, 100},
		{"above", 300, -900, 300, -768},
		{"below", 300, 1200, 300, 1080},
		{"right, offset down", 1950, 600, 1920, 600},
		{"right, too far down keeps a touch", 2600, 1100, 1920, 1080 - minTouch},
		{"inside the PC screen goes to the nearest edge", 1800, 200, 1920, 200},
	}
	for _, c := range cases {
		x, y := snapLayout(oneMon, 1366, 768, c.x, c.y, 0)
		if x != c.wx || y != c.wy {
			t.Errorf("%s: got %d,%d want %d,%d", c.name, x, y, c.wx, c.wy)
		}
	}
}

func TestSnapPull(t *testing.T) {
	// within the pull distance, edges line up: top, bottom, centre
	if x, y := snapLayout(oneMon, 1366, 768, 1930, 15, 30); x != 1920 || y != 0 {
		t.Errorf("top align: %d,%d", x, y)
	}
	if _, y := snapLayout(oneMon, 1366, 768, 1930, 1080-768-20, 30); y != 1080-768 {
		t.Errorf("bottom align: %d", y)
	}
	if _, y := snapLayout(oneMon, 1366, 600, 1930, 230, 30); y != 240 {
		t.Errorf("centre align: %d", y)
	}
}

func TestSnapNeverOverlapsAnyMonitor(t *testing.T) {
	two := []rectI{{0, 0, 1920, 1080}, {1920, 0, 1920, 1080}} // second monitor on the right
	for _, p := range [][2]int{{1900, 0}, {2500, 500}, {3900, 0}, {-50, 50}, {1000, -50}} {
		x, y := snapLayout(two, 1366, 768, p[0], p[1], 20)
		r := rectI{x, y, 1366, 768}
		for _, m := range two {
			if r.overlaps(m) {
				t.Errorf("from %v: %v overlaps %v", p, r, m)
			}
		}
	}
	// between the two monitors is taken, so dropping there lands outside
	if x, _ := snapLayout(two, 1366, 768, 3850, 0, 0); x != 3840 {
		t.Errorf("right of the second monitor: x=%d", x)
	}
}

func TestDefaultLayout(t *testing.T) {
	two := []rectI{{0, 0, 2560, 1440}, {-1920, 0, 1920, 1080}} // main monitor first
	g := makeGeo(two, 1920, 1080, savedLayout{})
	if g.L.X != 2560 || g.L.Y != (1440-1080)/2 {
		t.Errorf("default: %+v", g.L)
	}
	// a monitor already right of the main one: the laptop goes to the nearest free spot, still touching the main one
	busy := []rectI{{0, 0, 1920, 1080}, {1920, 0, 1920, 1080}}
	g = makeGeo(busy, 1080, 1920, savedLayout{})
	if g.L.overlaps(busy[0]) || g.L.overlaps(busy[1]) {
		t.Errorf("overlap: %+v", g.L)
	}
	if !touches(g.L, busy[0]) {
		t.Errorf("default spot not next to the main monitor: %+v", g.L)
	}
	if _, _, ok := g.enterAt(0, 540); !ok {
		t.Errorf("cannot cross from the main monitor's left edge: %+v", g.L)
	}
}

func TestCrossRight(t *testing.T) {
	g := makeGeo(oneMon, 1366, 768, savedLayout{X: 1920, Y: 200, Set: true})
	// cursor on the right edge pixel, inside the laptop's span
	lx, ly, ok := g.enterAt(1919, 500)
	if !ok || lx != 0 || ly != 300 {
		t.Fatalf("enter: %v %v %v", lx, ly, ok)
	}
	// above or below the laptop's span there is nothing to cross into
	if _, _, ok := g.enterAt(1919, 100); ok {
		t.Error("crossed above the laptop")
	}
	if _, _, ok := g.enterAt(1919, 1000); ok {
		t.Error("crossed below the laptop")
	}
	// the left edge is not next to the laptop
	if _, _, ok := g.enterAt(0, 500); ok {
		t.Error("crossed on the wrong edge")
	}
	// back: moving off the laptop's left edge lands on the PC, one pixel in
	x, y, ok := g.leaveAt(-3, 300)
	if !ok || x != 1916 || y != 500 {
		t.Fatalf("leave: %d %d %v", x, y, ok)
	}
	// off the laptop's left edge where the PC has no screen (laptop extends below): stays on the laptop
	g2 := makeGeo(oneMon, 1366, 768, savedLayout{X: 1920, Y: 900, Set: true})
	if _, _, ok := g2.leaveAt(-1, 500); ok {
		t.Error("left into empty space")
	}
	// inside the laptop: no leave
	if _, _, ok := g.leaveAt(10, 10); ok {
		t.Error("left while inside")
	}
	// other laptop edges don't touch the PC
	if _, _, ok := g.leaveAt(1366, 300); ok {
		t.Error("left via the far edge")
	}
}

func TestCrossAboveAndBelow(t *testing.T) {
	g := makeGeo(oneMon, 1366, 768, savedLayout{X: 200, Y: -768, Set: true})
	lx, ly, ok := g.enterAt(500, 0)
	if !ok || lx != 300 || ly != 767 {
		t.Fatalf("enter from top edge: %v %v %v", lx, ly, ok)
	}
	if x, y, ok := g.leaveAt(300, 770); !ok || x != 500 || y != 3 {
		t.Fatalf("leave down: %d %d %v", x, y, ok)
	}
	g = makeGeo(oneMon, 1366, 768, savedLayout{X: 200, Y: 1080, Set: true})
	if lx, ly, ok := g.enterAt(500, 1079); !ok || lx != 300 || ly != 0 {
		t.Fatalf("enter from bottom edge: %v %v %v", lx, ly, ok)
	}
	if _, _, ok := g.enterAt(100, 1079); ok {
		t.Error("crossed left of the laptop")
	}
}

func TestCrossHookPastEdge(t *testing.T) {
	// some hooks report the position before Windows clamps it, already past the edge
	g := makeGeo(oneMon, 1366, 768, savedLayout{X: -1366, Y: 0, Set: true})
	if lx, ly, ok := g.enterAt(-4, 100); !ok || lx != 1362 || ly != 100 {
		t.Fatalf("past edge: %v %v %v", lx, ly, ok)
	}
}

func TestCrossMultiMonitorGap(t *testing.T) {
	// a taller monitor on the left, primary on the right; laptop above the primary
	mons := []rectI{{-1080, -500, 1080, 1920}, {0, 0, 1920, 1080}}
	g := makeGeo(mons, 1366, 768, savedLayout{X: 100, Y: -768, Set: true})
	if g.L.X != 100 || g.L.Y != -768 {
		t.Fatalf("layout moved: %+v", g.L)
	}
	if _, _, ok := g.enterAt(500, 0); !ok {
		t.Error("cannot cross up")
	}
	// the portrait monitor's right edge at y=-300 faces empty space, not the laptop (laptop starts at x=100)
	if _, _, ok := g.enterAt(-1, -300); ok {
		t.Error("crossed from the portrait monitor into empty space")
	}
}

func TestLayoutSaveLoad(t *testing.T) {
	d := dirs{home: t.TempDir()}
	if l := loadLayout(d); l.Set {
		t.Fatal("fresh layout is set")
	}
	saveLayout(d, savedLayout{X: -1366, Y: 40, Set: true})
	if l := loadLayout(d); !l.Set || l.X != -1366 || l.Y != 40 {
		t.Fatalf("got %+v", l)
	}
}
