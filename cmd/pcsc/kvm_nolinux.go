//go:build !linux

package main

func (a *app) kvmRecv(evs []inEv) {}
func (a *app) kvmRelease()        {}
