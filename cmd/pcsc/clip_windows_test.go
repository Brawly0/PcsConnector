package main

import "testing"

func TestClipRoundTrip(t *testing.T) {
	old, _ := clipRead()
	defer clipWrite(old)
	if err := clipWrite("héllo ✓ 世界"); err != nil {
		t.Fatal(err)
	}
	got, err := clipRead()
	if err != nil || got != "héllo ✓ 世界" {
		t.Fatalf("got %q %v", got, err)
	}
}
