package main

import (
	"bytes"
	"testing"
)

func TestFastSealOpen(t *testing.T) {
	a := &app{key: deriveKey("ABCDE-FGHIJ"), done: make(chan struct{})}
	b := &app{key: deriveKey("ABCDE-FGHIJ"), done: make(chan struct{})}
	pkt := a.fastSeal(udpMove, []byte("0123456789abcdef"))
	typ, p, ok := b.fastOpen(pkt)
	if !ok || typ != udpMove || !bytes.Equal(p, []byte("0123456789abcdef")) {
		t.Fatalf("round trip failed: ok=%v typ=%c p=%q", ok, typ, p)
	}
	other := &app{key: deriveKey("ZZZZZ-ZZZZZ"), done: make(chan struct{})}
	if _, _, ok := other.fastOpen(pkt); ok {
		t.Fatal("a datagram sealed with another pairing code was accepted")
	}
	if _, _, ok := b.fastOpen(pkt[:10]); ok {
		t.Fatal("a truncated datagram was accepted")
	}
}
