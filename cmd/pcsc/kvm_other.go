//go:build !windows

package main

func startKVMHost(a *app) func() { return nil }
