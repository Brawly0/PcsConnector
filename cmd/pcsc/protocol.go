package main

// Encrypted framing. Wire frame: [u32 len][nonce 12][AES-256-GCM(type byte + payload)].
// The key is derived from the shared token, so a wrong token fails on the first frame.

import (
	"bufio"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"sync"
	"time"

	"golang.org/x/crypto/scrypt"
)

const (
	typJSON  byte = 0
	typChunk byte = 1
	maxFrame      = 4 << 20
)

func deriveKey(token string) []byte {
	k, err := scrypt.Key([]byte(token), []byte("pcsconnector-v1"), 16384, 8, 1, 32)
	if err != nil {
		panic(err)
	}
	return k
}

type session struct {
	c    net.Conn
	r    *bufio.Reader
	aead cipher.AEAD
	wmu  sync.Mutex
}

func newSession(c net.Conn, key []byte) *session {
	blk, _ := aes.NewCipher(key)
	aead, _ := cipher.NewGCM(blk)
	if tc, ok := c.(*net.TCPConn); ok {
		tc.SetNoDelay(true)
		tc.SetKeepAlive(true)
		tc.SetKeepAlivePeriod(10 * time.Second)
	}
	return &session{c: c, r: bufio.NewReaderSize(c, 64<<10), aead: aead}
}

func (s *session) write(typ byte, payload []byte) error {
	nonce := make([]byte, 12)
	if _, err := rand.Read(nonce); err != nil {
		return err
	}
	plain := make([]byte, 0, 1+len(payload))
	plain = append(append(plain, typ), payload...)
	ct := s.aead.Seal(nonce, nonce, plain, nil) // nonce || ciphertext+tag
	buf := make([]byte, 4, 4+len(ct))
	binary.BigEndian.PutUint32(buf, uint32(len(ct)))
	buf = append(buf, ct...)
	s.wmu.Lock()
	defer s.wmu.Unlock()
	_, err := s.c.Write(buf)
	return err
}

func (s *session) read() (byte, []byte, error) {
	var hdr [4]byte
	if _, err := io.ReadFull(s.r, hdr[:]); err != nil {
		return 0, nil, err
	}
	n := binary.BigEndian.Uint32(hdr[:])
	if n < 12+16+1 || n > maxFrame {
		return 0, nil, errors.New("bad frame length")
	}
	buf := make([]byte, n)
	if _, err := io.ReadFull(s.r, buf); err != nil {
		return 0, nil, err
	}
	plain, err := s.aead.Open(nil, buf[:12], buf[12:], nil)
	if err != nil {
		return 0, nil, errors.New("authentication failed (wrong token?)")
	}
	return plain[0], plain[1:], nil
}

func (s *session) close() { s.c.Close() }
