//go:build windows

package main

import (
	"math"
	"os"
	"os/exec"
	"runtime"
	"sync"
	"testing"
	"time"
	"unsafe"
)

// TestCaptureLive records the PC's real output while Windows plays a sound. It makes noise, so it only
// runs with PCSC_AUDIO_TEST=1. With mute it also checks the loopback still hears the sound.
func TestCaptureLive(t *testing.T) {
	if os.Getenv("PCSC_AUDIO_TEST") == "" {
		t.Skip("set PCSC_AUDIO_TEST=1 (plays a sound)")
	}
	for _, mute := range []bool{false, true} {
		var mu sync.Mutex
		rate, n, peak := 0, 0, 0
		stop := startCapture(mute, func(r int) { mu.Lock(); rate = r; mu.Unlock() }, func(p []byte) {
			mu.Lock()
			n += len(p) / 4
			for i := 0; i+1 < len(p); i += 2 {
				v := int(s16(p, i/2))
				if v < 0 {
					v = -v
				}
				peak = max(peak, v)
			}
			mu.Unlock()
		}, func(err error) { t.Log(err) })
		time.Sleep(300 * time.Millisecond)
		exec.Command("powershell", "-NoProfile", "-Command",
			`$p = New-Object Media.SoundPlayer 'C:\Windows\Media\Windows Notify System Generic.wav'; $p.PlaySync(); $p.PlaySync()`).Run()
		time.Sleep(300 * time.Millisecond)
		stop()
		mu.Lock()
		t.Logf("mute=%v: rate %d, %d frames (%.2f s), peak %d", mute, rate, n, float64(n)/float64(max(rate, 1)), peak)
		if rate == 0 || n == 0 || peak < 300 {
			t.Errorf("mute=%v: nothing captured", mute)
		}
		mu.Unlock()
	}
}

// pcMuted reads the default output device's mute switch.
func pcMuted(t *testing.T) bool {
	pCoInit.Call(0, 0)
	var enum, dev, vol uintptr
	pCoCreate.Call(uintptr(unsafe.Pointer(&clsidMMDeviceEnumerator)), 0, clsctxAll, uintptr(unsafe.Pointer(&iidIMMDeviceEnumerator)), uintptr(unsafe.Pointer(&enum)))
	defer comRelease(enum)
	comCall(enum, 4, 0, 0, uintptr(unsafe.Pointer(&dev)))
	defer comRelease(dev)
	if comCall(dev, 3, uintptr(unsafe.Pointer(&iidIAudioEndpointVolume)), clsctxAll, 0, uintptr(unsafe.Pointer(&vol))) < 0 {
		t.Fatal("no endpoint volume")
	}
	defer comRelease(vol)
	var m int32
	comCall(vol, 15, uintptr(unsafe.Pointer(&m)))
	return m != 0
}

func TestCaptureMuteRestores(t *testing.T) {
	if os.Getenv("PCSC_AUDIO_TEST") == "" {
		t.Skip("set PCSC_AUDIO_TEST=1")
	}
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	before := pcMuted(t)
	stop := startCapture(true, func(int) {}, func([]byte) {}, func(error) {})
	time.Sleep(500 * time.Millisecond)
	during := pcMuted(t)
	stop()
	after := pcMuted(t)
	t.Logf("muted before=%v during=%v after=%v", before, during, after)
	if !during || after != before {
		t.Error("mute not applied or not restored")
	}
}

func TestUnmuteAfterCrash(t *testing.T) {
	if os.Getenv("PCSC_AUDIO_TEST") == "" {
		t.Skip("set PCSC_AUDIO_TEST=1")
	}
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	if pcMuted(t) {
		t.Skip("the PC is muted already")
	}
	home := t.TempDir()
	defer func() { muteMarker = "" }()
	// simulate a crash mid-send: muted, marker left behind, nothing restored
	muteMarker = home + `\sound-muted`
	stop := startCapture(true, func(int) {}, func([]byte) {}, func(error) {})
	time.Sleep(300 * time.Millisecond)
	if _, err := os.Stat(muteMarker); err != nil || !pcMuted(t) {
		stop()
		t.Fatal("marker missing or not muted while sending")
	}
	b, _ := os.ReadFile(muteMarker)
	stop()                             // restores and removes the marker...
	os.WriteFile(muteMarker, b, 0o644) // ...so put it back
	setMuteForTest(t, true)            // and mute again, as a crash would have left it
	unmuteAfterCrash(home)
	if pcMuted(t) {
		t.Error("still muted")
	}
	if _, err := os.Stat(muteMarker); err == nil {
		t.Error("marker not removed")
	}
}

func setMuteForTest(t *testing.T, on bool) {
	var enum, dev, vol uintptr
	pCoCreate.Call(uintptr(unsafe.Pointer(&clsidMMDeviceEnumerator)), 0, clsctxAll, uintptr(unsafe.Pointer(&iidIMMDeviceEnumerator)), uintptr(unsafe.Pointer(&enum)))
	defer comRelease(enum)
	comCall(enum, 4, 0, 0, uintptr(unsafe.Pointer(&dev)))
	defer comRelease(dev)
	comCall(dev, 3, uintptr(unsafe.Pointer(&iidIAudioEndpointVolume)), clsctxAll, 0, uintptr(unsafe.Pointer(&vol)))
	defer comRelease(vol)
	v := uintptr(0)
	if on {
		v = 1
	}
	comCall(vol, 14, v, 0)
}

// TestPlayerLive plays a 1 s tone through the waveOut player and records the PC's output meanwhile:
// the tone must come out, at about the right level and length.
func TestPlayerLive(t *testing.T) {
	if os.Getenv("PCSC_AUDIO_TEST") == "" {
		t.Skip("set PCSC_AUDIO_TEST=1 (plays a tone)")
	}
	var mu sync.Mutex
	loud := 0 // captured frames above a threshold
	stop := startCapture(false, func(int) {}, func(p []byte) {
		mu.Lock()
		for i := 0; i+3 < len(p); i += 4 {
			if v := s16(p, i/2); v > 2000 || v < -2000 {
				loud++
			}
		}
		mu.Unlock()
	}, func(err error) { t.Log(err) })
	defer stop()
	time.Sleep(300 * time.Millisecond)
	pl, err := startPlayer(48000)
	if err != nil {
		t.Fatal(err)
	}
	// 1 s of 440 Hz at 1/4 volume, fed in real time as 20 ms packets like the network does
	for k := 0; k < 50; k++ {
		pkt := make([]byte, 0, 3840)
		for i := 0; i < 960; i++ {
			n := k*960 + i
			v := int16(8000 * math.Sin(2*math.Pi*440*float64(n)/48000))
			pkt = append(pkt, byte(v), byte(uint16(v)>>8), byte(v), byte(uint16(v)>>8))
		}
		if !pl.write(pkt) {
			t.Fatal("player died")
		}
		time.Sleep(20 * time.Millisecond)
	}
	time.Sleep(300 * time.Millisecond)
	pl.close()
	mu.Lock()
	defer mu.Unlock()
	t.Logf("loud frames captured: %d (~%.2f s of tone peaks)", loud, float64(loud)/48000)
	if loud < 5000 {
		t.Error("the tone did not come out of the speakers")
	}
}
