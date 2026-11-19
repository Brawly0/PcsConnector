package main

// Pairing by code alone: the laptop finds the PC on the local network, so nobody has to type an address.
//
// The laptop broadcasts a query datagram to UDP <port> sealed with the key derived from the pairing code.
// Only a PC showing the same code can open it; that PC answers (sealed the same way) with its name and
// TCP port, and the laptop connects to the address the answer came from. Strangers on the network learn
// nothing: without the code a query is noise, and nothing answers it.
//
// Broadcasts stay on the local network. Across a VPN such as Tailscale, enter the PC's address instead.

import (
	"crypto/rand"
	"encoding/binary"
	"net"
	"time"
)

const (
	udpQuery  byte = 'q' // laptop -> broadcast: 8 random bytes (echoed back, ties the answer to the query)
	udpAnswer byte = 'a' // PC -> laptop: echoed 8 bytes, u16 TCP port, PC name
)

type found struct {
	ip, name string
	port     int
}

// answerQuery (PC) replies to a query that opened with our key.
func (a *app) answerQuery(c *net.UDPConn, from *net.UDPAddr, p []byte, tcpPort int) {
	if len(p) != 8 {
		return
	}
	out := make([]byte, 0, 10+64)
	out = append(out, p...)
	out = binary.BigEndian.AppendUint16(out, uint16(tcpPort))
	name := hostname()
	if len(name) > 64 {
		name = name[:64]
	}
	out = append(out, name...)
	c.WriteToUDP(a.fastSeal(udpAnswer, out), from)
}

// fastHostRead (PC) reads the UDP socket: discovery queries from laptops that have our code.
func (a *app) fastHostRead(c *net.UDPConn, tcpPort int) {
	buf := make([]byte, 512)
	for {
		n, from, err := c.ReadFromUDP(buf)
		if err != nil {
			if a.stopped() {
				return
			}
			continue
		}
		if typ, p, ok := a.fastOpen(buf[:n]); ok && typ == udpQuery {
			a.answerQuery(c, from, p, tcpPort)
		}
	}
}

// broadcastAddrs lists the limited broadcast address plus each up IPv4 interface's subnet broadcast,
// so the query goes out on every network the laptop is on, not only the default route.
func broadcastAddrs() []net.IP {
	out := []net.IP{net.IPv4bcast, net.IPv4(127, 0, 0, 1)} // loopback: a PC app on this same machine (tests)
	ifs, _ := net.Interfaces()
	for _, ifc := range ifs {
		if ifc.Flags&net.FlagUp == 0 || ifc.Flags&net.FlagBroadcast == 0 || ifc.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, _ := ifc.Addrs()
		for _, ad := range addrs {
			n, ok := ad.(*net.IPNet)
			if !ok || n.IP.To4() == nil || len(n.Mask) != net.IPv4len {
				continue
			}
			ip := n.IP.To4()
			b := make(net.IP, 4)
			for i := range b {
				b[i] = ip[i] | ^n.Mask[i]
			}
			out = append(out, b)
		}
	}
	return out
}

// discover (laptop) looks for the PC showing our code, for up to wait. It uses the fast-path socket when
// it has one (its port is the one install.sh opens in the firewall), else a temporary socket.
func (a *app) discover(port int, wait time.Duration) (found, bool) {
	a.fast.mu.Lock()
	c := a.fast.conn
	a.fast.mu.Unlock()
	answers := a.answers()
	if c == nil {
		tmp, err := net.ListenUDP("udp4", nil)
		if err != nil {
			return found{}, false
		}
		defer tmp.Close()
		c = tmp
		go func() {
			buf := make([]byte, 512)
			for {
				n, from, err := tmp.ReadFromUDP(buf)
				if err != nil {
					return
				}
				a.gotAnswer(from, buf[:n])
			}
		}()
	}
	nonce := make([]byte, 8)
	rand.Read(nonce)
	q := a.fastSeal(udpQuery, nonce)
	deadline := time.After(wait)
	tick := time.NewTicker(700 * time.Millisecond)
	defer tick.Stop()
	send := func() {
		for _, ip := range broadcastAddrs() {
			c.WriteToUDP(q, &net.UDPAddr{IP: ip, Port: port})
		}
	}
	send()
	for {
		select {
		case f := <-answers:
			if f.nonce == string(nonce) {
				return f.found, true
			}
		case <-tick.C:
			send()
		case <-deadline:
			return found{}, false
		case <-a.done:
			return found{}, false
		}
	}
}

type answer struct {
	found
	nonce string
}

func (a *app) answers() chan answer {
	a.fast.mu.Lock()
	defer a.fast.mu.Unlock()
	if a.fast.ans == nil {
		a.fast.ans = make(chan answer, 8)
	}
	return a.fast.ans
}

// gotAnswer (laptop) passes a discovery answer to discover.
func (a *app) gotAnswer(from *net.UDPAddr, b []byte) {
	typ, p, ok := a.fastOpen(b)
	if !ok || typ != udpAnswer || len(p) < 10 {
		return
	}
	f := answer{found{ip: from.IP.String(), port: int(binary.BigEndian.Uint16(p[8:10])), name: string(p[10:])}, string(p[:8])}
	select {
	case a.answers() <- f:
	default:
	}
}
