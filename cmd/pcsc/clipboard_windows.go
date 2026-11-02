//go:build windows

package main

import (
	"errors"
	"strings"
	"syscall"
	"time"
	"unsafe"
)

var (
	user32   = syscall.NewLazyDLL("user32.dll")
	kernel32 = syscall.NewLazyDLL("kernel32.dll")

	pOpen   = user32.NewProc("OpenClipboard")
	pClose  = user32.NewProc("CloseClipboard")
	pEmpty  = user32.NewProc("EmptyClipboard")
	pGet    = user32.NewProc("GetClipboardData")
	pSet    = user32.NewProc("SetClipboardData")
	pSeq    = user32.NewProc("GetClipboardSequenceNumber")
	pAlloc  = kernel32.NewProc("GlobalAlloc")
	pLock   = kernel32.NewProc("GlobalLock")
	pUnlock = kernel32.NewProc("GlobalUnlock")
)

const (
	cfUnicodeText = 13
	gmemMoveable  = 0x2
)

func openClip() error {
	for i := 0; i < 10; i++ {
		if r, _, _ := pOpen.Call(0); r != 0 {
			return nil
		}
		time.Sleep(10 * time.Millisecond)
	}
	return errors.New("clipboard busy")
}

func clipSeq() (uint32, bool) {
	r, _, _ := pSeq.Call()
	return uint32(r), true
}

func clipRead() (string, error) {
	if err := openClip(); err != nil {
		return "", err
	}
	defer pClose.Call()
	h, _, _ := pGet.Call(cfUnicodeText)
	if h == 0 {
		return "", nil // no text on the clipboard
	}
	p, _, _ := pLock.Call(h)
	if p == 0 {
		return "", errors.New("GlobalLock failed")
	}
	defer pUnlock.Call(h)
	n := 0
	for n < maxClip && *(*uint16)(unsafe.Add(unsafe.Pointer(p), n*2)) != 0 {
		n++
	}
	return syscall.UTF16ToString(unsafe.Slice((*uint16)(unsafe.Pointer(p)), n)), nil
}

func clipWrite(text string) error {
	u, err := syscall.UTF16FromString(strings.ReplaceAll(text, "\x00", ""))
	if err != nil {
		return err
	}
	if err := openClip(); err != nil {
		return err
	}
	defer pClose.Call()
	pEmpty.Call()
	h, _, _ := pAlloc.Call(gmemMoveable, uintptr(len(u)*2))
	if h == 0 {
		return errors.New("GlobalAlloc failed")
	}
	p, _, _ := pLock.Call(h)
	if p == 0 {
		return errors.New("GlobalLock failed")
	}
	copy(unsafe.Slice((*uint16)(unsafe.Pointer(p)), len(u)), u)
	pUnlock.Call(h)
	if r, _, _ := pSet.Call(cfUnicodeText, h); r == 0 {
		return errors.New("SetClipboardData failed")
	}
	return nil // the OS owns the memory now
}
