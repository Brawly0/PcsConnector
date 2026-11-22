//go:build windows

package main

// PC sound capture: WASAPI loopback records whatever the default output device is playing. Called through
// COM vtables with syscall, no cgo. Output is 16-bit stereo PCM at the device's mix rate.

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"syscall"
	"time"
	"unsafe"
)

var (
	ole32          = syscall.NewLazyDLL("ole32.dll")
	pCoInit        = ole32.NewProc("CoInitializeEx")
	pCoUninit      = ole32.NewProc("CoUninitialize")
	pCoCreate      = ole32.NewProc("CoCreateInstance")
	pCoTaskMemFree = ole32.NewProc("CoTaskMemFree")

	clsidMMDeviceEnumerator = mkGUID(0xBCDE0395, 0xE52F, 0x467C, 0x8E, 0x3D, 0xC4, 0x57, 0x92, 0x91, 0x69, 0x2E)
	iidIMMDeviceEnumerator  = mkGUID(0xA95664D2, 0x9614, 0x4F35, 0xA7, 0x46, 0xDE, 0x8D, 0xB6, 0x36, 0x17, 0xE6)
	iidIAudioClient         = mkGUID(0x1CB9AD4C, 0xDBFA, 0x4C32, 0xB1, 0x78, 0xC2, 0xF5, 0x68, 0xA7, 0x03, 0xB2)
	iidIAudioCaptureClient  = mkGUID(0xC8ADBD64, 0xE71E, 0x48A0, 0xA4, 0xDE, 0x18, 0x5C, 0x39, 0x5C, 0xD3, 0x17)
	iidIAudioEndpointVolume = mkGUID(0x5CDF2C82, 0x841E, 0x4546, 0x97, 0x22, 0x0C, 0xF7, 0x40, 0x78, 0x22, 0x9A)
)

type guid struct {
	d1 uint32
	d2 uint16
	d3 uint16
	d4 [8]byte
}

func mkGUID(d1 uint32, d2, d3 uint16, b ...byte) guid {
	g := guid{d1: d1, d2: d2, d3: d3}
	copy(g.d4[:], b)
	return g
}

type waveFormatEx struct {
	tag         uint16
	channels    uint16
	rate        uint32
	avgBytes    uint32
	blockAlign  uint16
	bits        uint16
	cbSize      uint16
	validBits   uint16 // WAVEFORMATEXTENSIBLE from here (cbSize >= 22)
	channelMask uint32
	subFormat   guid
}

// comCall calls method idx of a COM object's vtable.
func comCall(obj uintptr, idx int, args ...uintptr) int32 {
	vtbl := *(*uintptr)(unsafe.Pointer(obj))
	fn := *(*uintptr)(unsafe.Pointer(vtbl + uintptr(idx)*unsafe.Sizeof(uintptr(0))))
	r, _, _ := syscall.SyscallN(fn, append([]uintptr{obj}, args...)...)
	return int32(r)
}

func comRelease(obj uintptr) {
	if obj != 0 {
		comCall(obj, 2)
	}
}

func hrErr(hr int32, what string) error {
	if hr < 0 {
		return fmt.Errorf("%s failed (0x%08X)", what, uint32(hr))
	}
	return nil
}

const clsctxAll = 0x17

// captureOnce runs one capture session on the current default output device. It returns nil when quit
// closes, or an error (device unplugged or switched, ...) so the caller can start over.
func captureOnce(quit chan struct{}, mute bool, onFormat func(int), onPCM func([]byte)) error {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	pCoInit.Call(0, 0) // COINIT_MULTITHREADED
	defer pCoUninit.Call()

	var enum, dev, client, capt uintptr
	r, _, _ := pCoCreate.Call(uintptr(unsafe.Pointer(&clsidMMDeviceEnumerator)), 0, clsctxAll,
		uintptr(unsafe.Pointer(&iidIMMDeviceEnumerator)), uintptr(unsafe.Pointer(&enum)))
	if err := hrErr(int32(r), "audio device list"); err != nil {
		return err
	}
	defer comRelease(enum)
	if err := hrErr(comCall(enum, 4, 0, 0, uintptr(unsafe.Pointer(&dev))), "default output device"); err != nil { // eRender, eConsole
		return err
	}
	defer comRelease(dev)
	if err := hrErr(comCall(dev, 3, uintptr(unsafe.Pointer(&iidIAudioClient)), clsctxAll, 0, uintptr(unsafe.Pointer(&client))), "audio client"); err != nil {
		return err
	}
	defer comRelease(client)
	var wf *waveFormatEx
	if err := hrErr(comCall(client, 8, uintptr(unsafe.Pointer(&wf))), "mix format"); err != nil {
		return err
	}
	defer pCoTaskMemFree.Call(uintptr(unsafe.Pointer(wf)))
	isFloat := wf.tag == 3 || (wf.tag == 0xFFFE && wf.cbSize >= 22 && wf.subFormat.d1 == 3)
	isPCM := wf.tag == 1 || (wf.tag == 0xFFFE && wf.cbSize >= 22 && wf.subFormat.d1 == 1)
	ch := int(wf.channels)
	if ch < 1 || !(isFloat && wf.bits == 32) && !(isPCM && wf.bits == 16) {
		return fmt.Errorf("unsupported sound format (tag %#x, %d bits)", wf.tag, wf.bits)
	}
	const loopback = 0x00020000
	if err := hrErr(comCall(client, 3, 0, loopback, 2_000_000, 0, uintptr(unsafe.Pointer(wf)), 0), "starting loopback"); err != nil { // shared, 200 ms
		return err
	}
	if err := hrErr(comCall(client, 14, uintptr(unsafe.Pointer(&iidIAudioCaptureClient)), uintptr(unsafe.Pointer(&capt))), "capture client"); err != nil {
		return err
	}
	defer comRelease(capt)

	if mute {
		var vol uintptr
		if comCall(dev, 3, uintptr(unsafe.Pointer(&iidIAudioEndpointVolume)), clsctxAll, 0, uintptr(unsafe.Pointer(&vol))) >= 0 {
			var was int32
			comCall(vol, 15, uintptr(unsafe.Pointer(&was)))
			if was == 0 && muteMarker != "" {
				os.WriteFile(muteMarker, nil, 0o644) // so a crash doesn't leave the PC muted: see unmuteAfterCrash
			}
			comCall(vol, 14, 1, 0)
			defer func() {
				comCall(vol, 14, uintptr(was), 0)
				comRelease(vol)
				if muteMarker != "" {
					os.Remove(muteMarker)
				}
			}()
		}
	}

	onFormat(int(wf.rate))
	if err := hrErr(comCall(client, 10), "starting capture"); err != nil {
		return err
	}
	defer comCall(client, 11)

	out := make([]byte, 0, 16<<10)
	for {
		select {
		case <-quit:
			return nil
		case <-time.After(5 * time.Millisecond):
		}
		for {
			var n uint32
			if err := hrErr(comCall(capt, 5, uintptr(unsafe.Pointer(&n))), "reading sound"); err != nil {
				return err
			}
			if n == 0 {
				break
			}
			var data uintptr
			var frames, flags uint32
			if err := hrErr(comCall(capt, 3, uintptr(unsafe.Pointer(&data)), uintptr(unsafe.Pointer(&frames)), uintptr(unsafe.Pointer(&flags)), 0, 0), "reading sound"); err != nil {
				return err
			}
			out = out[:0]
			if flags&2 != 0 || data == 0 { // AUDCLNT_BUFFERFLAGS_SILENT
				out = append(out, make([]byte, int(frames)*4)...)
			} else if isFloat {
				out = toStereo16(out, unsafe.Slice((*float32)(unsafe.Pointer(data)), int(frames)*ch), ch)
			} else {
				out = pcm16Stereo(out, unsafe.Slice((*int16)(unsafe.Pointer(data)), int(frames)*ch), ch)
			}
			comCall(capt, 4, uintptr(frames))
			onPCM(out)
		}
	}
}

// unmuteAfterCrash: if the marker is still there, the app died while sending sound with the speakers
// muted. Unmute them.
func unmuteAfterCrash(home string) {
	muteMarker = filepath.Join(home, "sound-muted")
	if _, err := os.Stat(muteMarker); err != nil {
		return
	}
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	pCoInit.Call(0, 0)
	defer pCoUninit.Call()
	var enum, dev, vol uintptr
	if r, _, _ := pCoCreate.Call(uintptr(unsafe.Pointer(&clsidMMDeviceEnumerator)), 0, clsctxAll, uintptr(unsafe.Pointer(&iidIMMDeviceEnumerator)), uintptr(unsafe.Pointer(&enum))); int32(r) < 0 {
		return
	}
	defer comRelease(enum)
	if comCall(enum, 4, 0, 0, uintptr(unsafe.Pointer(&dev))) < 0 {
		return
	}
	defer comRelease(dev)
	if comCall(dev, 3, uintptr(unsafe.Pointer(&iidIAudioEndpointVolume)), clsctxAll, 0, uintptr(unsafe.Pointer(&vol))) < 0 {
		return
	}
	comCall(vol, 14, 0, 0)
	comRelease(vol)
	os.Remove(muteMarker)
	log.Print("Unmuted the PC's speakers (the app had stopped while sending sound)")
}
