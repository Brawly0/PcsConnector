//go:build !windows

package main

import "errors"

type monitor struct {
	X, Y, W, H int
	Primary    bool
	Virtual    bool
	Name       string
}

func listMonitors() []monitor         { return nil }
func virtualMonitor() (monitor, bool) { return monitor{}, false }
func hasVirtualDriver() bool          { return false }

type grabber struct{}

func newGrabber(x, y, w, h int) (*grabber, error) {
	return nil, errors.New("screen capture is Windows-only")
}
func (g *grabber) grab(buf []byte) bool { return false }
func (g *grabber) close()               {}
