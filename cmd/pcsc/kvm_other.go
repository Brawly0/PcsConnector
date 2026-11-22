//go:build !windows

package main

func startKVMHost(a *app) func() { return nil }

// nativeScreen: not known natively here; the app window reports the size instead (/api/screensize).
func nativeScreen() (int, int) { return 0, 0 }
