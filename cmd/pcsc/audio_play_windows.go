//go:build windows

package main

// Playing the other computer's sound on Windows: waveOut (winmm) with a small ring of 20 ms buffers.
// waveOut goes to the default output device and shows up in the volume mixer as PcsConnector.

import (
	"fmt"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
	"unsafe"
)

var (
	winmm             = syscall.NewLazyDLL("winmm.dll")
	pWaveOutOpen      = winmm.NewProc("waveOutOpen")
	pWaveOutPrepare   = winmm.NewProc("waveOutPrepareHeader")
	pWaveOutUnprepare = winmm.NewProc("waveOutUnprepareHeader")
	pWaveOutWrite     = winmm.NewProc("waveOutWrite")
	pWaveOutReset     = winmm.NewProc("waveOutReset")
	pWaveOutClose     = winmm.NewProc("waveOutClose")
)

type waveHdr struct {
	data     uintptr
	bufLen   uint32
	recorded uint32
	user     uintptr
	flags    uint32
	loops    uint32
	next     uintptr
	reserved uintptr
}

const (
	whdrDone     = 0x01
	whdrPrepared = 0x02
	waveRing     = 4 // buffers handed to Windows at once: at most ~80 ms queued in the device
)

type waveOutPlayer struct {
	h    uintptr
	hdrs [waveRing]waveHdr
	bufs [waveRing][]byte
	used [waveRing]bool
	jq   *jitterQueue
	quit chan struct{}
	done chan struct{}
	dead atomic.Bool
	once sync.Once
}

func startPlayer(rate int) (audioSink, error) {
	wf := waveFormatEx{tag: 1, channels: 2, rate: uint32(rate), avgBytes: uint32(rate * 4), blockAlign: 4, bits: 16}
	p := &waveOutPlayer{jq: newJitterQueue(5, 2), quit: make(chan struct{}), done: make(chan struct{})}
	const waveMapper = ^uintptr(0) // WAVE_MAPPER (-1): the default output device
	if r, _, _ := pWaveOutOpen.Call(uintptr(unsafe.Pointer(&p.h)), waveMapper, uintptr(unsafe.Pointer(&wf)), 0, 0, 0); r != 0 {
		return nil, fmt.Errorf("cannot open the sound output (error %d)", r)
	}
	for i := range p.bufs {
		p.bufs[i] = make([]byte, 16<<10)
	}
	go p.run()
	return p, nil
}

func (p *waveOutPlayer) run() {
	defer close(p.done)
	next := 0
	for {
		var b []byte
		select {
		case <-p.quit:
			return
		case b = <-p.jq.q:
		}
		// wait for the next buffer in the ring to come back from the device
		h := &p.hdrs[next]
		for p.used[next] && atomic.LoadUint32(&h.flags)&whdrDone == 0 {
			select {
			case <-p.quit:
				return
			case <-time.After(2 * time.Millisecond):
			}
		}
		if p.used[next] {
			pWaveOutUnprepare.Call(p.h, uintptr(unsafe.Pointer(h)), unsafe.Sizeof(*h))
		}
		n := copy(p.bufs[next], b)
		*h = waveHdr{data: uintptr(unsafe.Pointer(&p.bufs[next][0])), bufLen: uint32(n)}
		if r, _, _ := pWaveOutPrepare.Call(p.h, uintptr(unsafe.Pointer(h)), unsafe.Sizeof(*h)); r != 0 {
			p.dead.Store(true)
			return
		}
		if r, _, _ := pWaveOutWrite.Call(p.h, uintptr(unsafe.Pointer(h)), unsafe.Sizeof(*h)); r != 0 {
			p.dead.Store(true)
			return
		}
		p.used[next] = true
		next = (next + 1) % waveRing
	}
}

func (p *waveOutPlayer) write(b []byte) bool {
	if p.dead.Load() {
		return false
	}
	p.jq.push(b)
	return true
}

func (p *waveOutPlayer) close() {
	p.once.Do(func() {
		close(p.quit)
		<-p.done
		pWaveOutReset.Call(p.h) // marks every queued buffer done
		for i := range p.hdrs {
			if p.used[i] {
				pWaveOutUnprepare.Call(p.h, uintptr(unsafe.Pointer(&p.hdrs[i])), unsafe.Sizeof(p.hdrs[i]))
			}
		}
		pWaveOutClose.Call(p.h)
	})
}
