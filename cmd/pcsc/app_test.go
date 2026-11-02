package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func testDirs(t *testing.T) dirs {
	h := t.TempDir()
	return dirs{h, filepath.Join(h, "Inbox"), filepath.Join(h, "Outbox"), filepath.Join(h, "Sent")}
}

func TestFileTransferLoopback(t *testing.T) {
	host, err := newApp(testDirs(t), "tok", true)
	if err != nil {
		t.Fatal(err)
	}
	join, err := newApp(testDirs(t), "tok", true)
	if err != nil {
		t.Fatal(err)
	}
	go host.serve(24897)
	time.Sleep(200 * time.Millisecond)
	go join.dial("127.0.0.1", 24897)

	data := make([]byte, 1_000_000)
	for i := range data {
		data[i] = byte(i)
	}
	os.WriteFile(filepath.Join(join.dirs.outbox, "hello.bin"), data, 0o644)

	got := filepath.Join(host.dirs.inbox, "hello.bin")
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if b, err := os.ReadFile(got); err == nil && len(b) == len(data) {
			if string(b) != string(data) {
				t.Fatal("content mismatch")
			}
			return
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Fatal("file did not arrive")
}

func TestWrongTokenRejected(t *testing.T) {
	a, _ := newApp(testDirs(t), "right", true)
	b, _ := newApp(testDirs(t), "wrong", true)
	go a.serve(24898)
	time.Sleep(200 * time.Millisecond)
	go b.dial("127.0.0.1", 24898)
	time.Sleep(1500 * time.Millisecond)
	if a.current() != nil && b.current() != nil {
		// sessions may exist briefly; the read loop must have exited on auth failure
		time.Sleep(500 * time.Millisecond)
	}
	os.WriteFile(filepath.Join(b.dirs.outbox, "x.txt"), []byte("secret"), 0o644)
	time.Sleep(3 * time.Second)
	if _, err := os.Stat(filepath.Join(a.dirs.inbox, "x.txt")); err == nil {
		t.Fatal("file crossed with wrong token")
	}
}
