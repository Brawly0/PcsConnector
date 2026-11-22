//go:build !windows && !linux

package main

import "errors"

var errNoSound = errors.New("sound sharing is not supported on this system")

func captureOnce(quit chan struct{}, mute bool, onFormat func(int), onPCM func([]byte)) error {
	return errNoSound
}

func startPlayer(rate int) (audioSink, error) { return nil, errNoSound }

func unmuteAfterCrash(home string) {}
