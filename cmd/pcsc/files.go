package main

// File transfer by folder: drop a file in Outbox -> it appears in the peer's Inbox.

import (
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"time"
)

const chunkSize = 256 << 10

func uniquePath(dir, name string) string {
	ext := filepath.Ext(name)
	base := name[:len(name)-len(ext)]
	p := filepath.Join(dir, name)
	for i := 1; ; i++ {
		if _, err := os.Stat(p); os.IsNotExist(err) {
			return p
		}
		p = filepath.Join(dir, fmt.Sprintf("%s (%d)%s", base, i, ext))
	}
}

// receiver writes incoming files to the inbox (basename only, never overwrites).
type receiver struct {
	inbox string
	f     *os.File
	got   int64
}

func (r *receiver) start(name string) {
	r.abort()
	name = filepath.Base(name)
	if name == "." || name == ".." || name == string(filepath.Separator) {
		name = "file"
	}
	f, err := os.Create(uniquePath(r.inbox, name))
	if err != nil {
		log.Printf("cannot create %s: %v", name, err)
		return
	}
	r.f, r.got = f, 0
}

func (r *receiver) chunk(b []byte) {
	if r.f == nil {
		return
	}
	n, _ := r.f.Write(b)
	r.got += int64(n)
}

func (r *receiver) end() {
	if r.f == nil {
		return
	}
	name := r.f.Name()
	r.f.Close()
	r.f = nil
	log.Printf("received %s (%d bytes)", filepath.Base(name), r.got)
}

func (r *receiver) abort() {
	if r.f == nil {
		return
	}
	name := r.f.Name()
	r.f.Close()
	os.Remove(name)
	r.f = nil
}

// watchOutbox sends settled files (size unchanged between polls) one at a time, then moves them to Sent.
func (a *app) watchOutbox() {
	seen := map[string]int64{}
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	for {
		select {
		case <-a.done:
			return
		case <-tick.C:
		}
		s := a.current()
		if s == nil {
			continue
		}
		entries, err := os.ReadDir(a.dirs.outbox)
		if err != nil {
			continue
		}
		for _, e := range entries {
			full := filepath.Join(a.dirs.outbox, e.Name())
			st, err := os.Stat(full)
			if err != nil || !st.Mode().IsRegular() {
				continue
			}
			if prev, ok := seen[full]; !ok || prev != st.Size() {
				seen[full] = st.Size()
				continue
			}
			delete(seen, full)
			if err := sendFile(s, full, e.Name(), st.Size()); err != nil {
				log.Printf("send failed for %s: %v", e.Name(), err)
				break
			}
			os.Rename(full, uniquePath(a.dirs.sent, e.Name()))
			log.Printf("sent %s (%d bytes)", e.Name(), st.Size())
			break // one per tick
		}
	}
}

func sendFile(s *session, path, name string, size int64) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	if err := s.send(msg{T: "file-start", Name: name, Size: size}); err != nil {
		return err
	}
	buf := make([]byte, chunkSize)
	for {
		n, err := f.Read(buf)
		if n > 0 {
			if werr := s.write(typChunk, buf[:n]); werr != nil {
				return werr
			}
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
	}
	return s.send(msg{T: "file-end"})
}
