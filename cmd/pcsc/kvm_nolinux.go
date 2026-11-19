//go:build !linux

package main

func (a *app) kvmRecv(evs []inEv)               {}
func (a *app) kvmRelease()                      {}
func (a *app) kvmMove(seq uint64, x, y float64) {}
