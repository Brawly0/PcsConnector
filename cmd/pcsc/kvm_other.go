//go:build !windows

package main

func startKVMHost(a *app, w, h, side int) func() { return nil }
