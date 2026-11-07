package main

// Input events sent from the laptop's screen viewer to the Windows PC.

type inEv struct {
	E string  `json:"e"` // m move, d button down, u button up, w wheel, kd/ku key down/up
	X float64 `json:"x,omitempty"`
	Y float64 `json:"y,omitempty"`
	B int     `json:"b,omitempty"` // mouse button 0/1/2, or 1 = extended key
	D int     `json:"d,omitempty"` // wheel delta
	K int     `json:"k,omitempty"` // Windows virtual-key code
}

// input runs on the Windows host; events are only accepted while the extended screen is on.
func (a *app) input(evs []inEv) {
	a.scr.mu.Lock()
	on, name := a.scr.stop != nil, a.scr.monName
	a.scr.mu.Unlock()
	if !on || len(evs) > 200 {
		return
	}
	if m, ok := monitorByName(name); ok {
		injectEvents(m, evs)
	}
}

// sendInput is called on the laptop with events from the viewer page.
func (a *app) sendInput(evs []inEv) {
	if s := a.current(); s != nil && a.screenOn() {
		s.send(msg{T: "input", Ev: evs})
	}
}
