//go:build !windows

package main

import "errors"

func vmonStatus() map[string]any { return map[string]any{"supported": false} }
func installDisplay() error      { return errors.New("the virtual monitor is set up on the Windows PC") }
func runInstallDisplay()         {}
