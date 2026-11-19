package main

// UDP fast path for mouse moves. Moves carry absolute positions, so a lost datagram is simply superseded
// by the next one, while over TCP one lost packet on Wi-Fi stalls every later move until it is resent.
// Buttons, keys, enter/leave stay on TCP. Every event carries a sequence number so the laptop never
// applies an older position after a newer one, whichever path it came over.
//
// The PC sends a probe datagram to the laptop (same port number, UDP) every second; the laptop answers
// over TCP with "udp-ok". Moves use UDP only while those answers keep arriving, so a blocked UDP path
// falls back to TCP by itself. The probe round trip doubles as the latency readout in the PC's window.
//
// Datagram: [nonce 12][AES-256-GCM(type byte + payload)], same key as the TCP session.

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/binary"
	"fmt"
	"log"
	"math"
	"net"
	"sync"
	"time"
)

const (
	udpProbe byte = 'p' // payload: int64 send time (unix ns)
	udpMove  byte = 'm' // payload: u64 seq, f32 x, f32 y
)

type fastState struct {
	mu   sync.Mutex
	conn *net.UDPConn
	port int
	aead cipher.AEAD
	okAt time.Time       // host: last udp-ok from the laptop
	rtts []time.Duration // host: recent probe round trips
	ans  chan answer     // laptop: discovery answers (discover.go)
}

func (a *app) fastAEAD() cipher.AEAD {
	a.fast.mu.Lock()
	defer a.fast.mu.Unlock()
	if a.fast.aead == nil {
		blk, _ := aes.NewCipher(a.key)
		a.fast.aead, _ = cipher.NewGCM(blk)
	}
	return a.fast.aead
}

func (a *app) fastSeal(typ byte, payload []byte) []byte {
	nonce := make([]byte, 12, 12+1+len(payload)+16)
	rand.Read(nonce)
	return a.fastAEAD().Seal(nonce, nonce, append([]byte{typ}, payload...), nil)
}

func (a *app) fastOpen(b []byte) (byte, []byte, bool) {
	if len(b) < 12+16+1 {
		return 0, nil, false
	}
	p, err := a.fastAEAD().Open(nil, b[:12], b[12:], nil)
	if err != nil {
		return 0, nil, false
	}
	return p[0], p[1:], true
}

// fastListen binds the UDP socket on both sides. Failure only costs the fast path.
func (a *app) fastListen(port int) *net.UDPConn {
	c, err := net.ListenUDP("udp", &net.UDPAddr{Port: port})
	if err != nil {
		log.Printf("Fast mouse path unavailable (UDP %d: %v); using the normal connection", port, err)
		return nil
	}
	a.fast.mu.Lock()
	a.fast.conn, a.fast.port = c, port
	a.fast.mu.Unlock()
	go func() {
		<-a.done
		c.Close()
	}()
	return c
}

// fastProbe (host) runs for one session, probing the laptop every second.
func (a *app) fastProbe(s *session, ip string) {
	a.fast.mu.Lock()
	c, port := a.fast.conn, a.fast.port
	a.fast.okAt, a.fast.rtts = time.Time{}, nil
	a.fast.mu.Unlock()
	if c == nil {
		return
	}
	dst := &net.UDPAddr{IP: net.ParseIP(ip), Port: port}
	for a.current() == s && !a.stopped() {
		var p [8]byte
		binary.BigEndian.PutUint64(p[:], uint64(time.Now().UnixNano()))
		c.WriteToUDP(a.fastSeal(udpProbe, p[:]), dst)
		select {
		case <-a.done:
		case <-time.After(time.Second):
		}
	}
}

// fastAck (host) records a probe answer.
func (a *app) fastAck(sent int64) {
	rtt := time.Since(time.Unix(0, sent))
	if rtt < 0 || rtt > 10*time.Second {
		return
	}
	a.fast.mu.Lock()
	a.fast.okAt = time.Now()
	a.fast.rtts = append(a.fast.rtts, rtt)
	if len(a.fast.rtts) > 10 {
		a.fast.rtts = a.fast.rtts[1:]
	}
	a.fast.mu.Unlock()
}

// fastMove (host) sends a move over UDP if the path is known to work.
func (a *app) fastMove(e inEv) bool {
	a.fast.mu.Lock()
	c, port, ok := a.fast.conn, a.fast.port, time.Since(a.fast.okAt) < 3500*time.Millisecond
	a.fast.mu.Unlock()
	_, ip := a.peerInfo()
	if c == nil || !ok || ip == "" {
		return false
	}
	var p [16]byte
	binary.BigEndian.PutUint64(p[0:], e.S)
	binary.BigEndian.PutUint32(p[8:], math.Float32bits(float32(e.X)))
	binary.BigEndian.PutUint32(p[12:], math.Float32bits(float32(e.Y)))
	_, err := c.WriteToUDP(a.fastSeal(udpMove, p[:]), &net.UDPAddr{IP: net.ParseIP(ip), Port: port})
	return err == nil
}

// fastStatus (host) summarises the link for the window: path and latency.
func (a *app) fastStatus() map[string]any {
	a.fast.mu.Lock()
	defer a.fast.mu.Unlock()
	st := map[string]any{"udp": time.Since(a.fast.okAt) < 3500*time.Millisecond}
	if n := len(a.fast.rtts); n > 0 {
		var sum, hi time.Duration
		for _, r := range a.fast.rtts {
			sum += r
			hi = max(hi, r)
		}
		st["ms"] = fmt.Sprintf("%.0f", float64(sum/time.Duration(n))/1e6)
		st["maxms"] = fmt.Sprintf("%.0f", float64(hi)/1e6)
	}
	return st
}

// fastLaptopRead (laptop) answers probes, applies moves and collects discovery answers.
func (a *app) fastLaptopRead(c *net.UDPConn) {
	buf := make([]byte, 512)
	for {
		n, from, err := c.ReadFromUDP(buf)
		if err != nil {
			if a.stopped() {
				return
			}
			continue
		}
		typ, p, ok := a.fastOpen(buf[:n])
		if !ok {
			continue
		}
		switch {
		case typ == udpAnswer:
			a.gotAnswer(from, buf[:n])
		case typ == udpProbe && len(p) == 8:
			if s := a.current(); s != nil {
				s.send(msg{T: "udp-ok", Size: int64(binary.BigEndian.Uint64(p))})
			}
		case typ == udpMove && len(p) == 16:
			a.kvmMove(binary.BigEndian.Uint64(p[0:]),
				float64(math.Float32frombits(binary.BigEndian.Uint32(p[8:]))),
				float64(math.Float32frombits(binary.BigEndian.Uint32(p[12:]))))
		}
	}
}
