package main

import (
	"encoding/binary"
	"testing"
)

func s16(b []byte, i int) int16 { return int16(binary.LittleEndian.Uint16(b[i*2:])) }

func TestToStereo16(t *testing.T) {
	// stereo: clamps and rounds
	out := toStereo16(nil, []float32{0, 1, -1, 0.5, 2, -2}, 2)
	want := []int16{0, 32767, -32767, 16384, 32767, -32768}
	if len(out) != len(want)*2 {
		t.Fatalf("len %d", len(out))
	}
	for i, w := range want {
		if g := s16(out, i); g != w {
			t.Errorf("sample %d: %d want %d", i, g, w)
		}
	}
	// mono is doubled
	out = toStereo16(nil, []float32{0.25, -0.25}, 1)
	if len(out) != 8 || s16(out, 0) != s16(out, 1) || s16(out, 2) != s16(out, 3) || s16(out, 0) != 8192 {
		t.Errorf("mono: %v", out)
	}
	// 5.1: keeps front left/right only
	out = toStereo16(nil, []float32{0.1, 0.2, 0.9, 0.9, 0.9, 0.9}, 6)
	if len(out) != 4 || s16(out, 0) != 3277 || s16(out, 1) != 6553 {
		t.Errorf("5.1: %d %d", s16(out, 0), s16(out, 1))
	}
	// partial trailing frame is ignored
	if out = toStereo16(nil, []float32{0.1, 0.2, 0.3}, 2); len(out) != 4 {
		t.Errorf("partial: %d", len(out))
	}
}

func TestPCM16Stereo(t *testing.T) {
	out := pcm16Stereo(nil, []int16{1, -2, 3, 9, 9, 9}, 3)
	if len(out) != 8 || s16(out, 0) != 1 || s16(out, 1) != -2 || s16(out, 2) != 9 || s16(out, 3) != 9 {
		t.Errorf("got %v", out)
	}
}

func TestJitterQueueBoundsDelay(t *testing.T) {
	j := newJitterQueue(6, 2)
	for i := 0; i < 5; i++ {
		j.push([]byte{byte(i)})
	}
	if len(j.q) != 5 {
		t.Fatalf("queued %d", len(j.q))
	}
	j.push([]byte{5}) // 6th fits
	j.push([]byte{6}) // burst: drop down to 2, then add
	if len(j.q) != 3 {
		t.Fatalf("after burst %d", len(j.q))
	}
	// the newest packets survive, in order
	for _, w := range []byte{4, 5, 6} {
		if b := <-j.q; b[0] != w {
			t.Errorf("got %d want %d", b[0], w)
		}
	}
	// push copies: the caller may reuse its buffer
	buf := []byte{7}
	j.push(buf)
	buf[0] = 8
	if b := <-j.q; b[0] != 7 {
		t.Error("push kept the caller's buffer")
	}
}
