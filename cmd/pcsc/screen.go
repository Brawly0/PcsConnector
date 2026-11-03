package main

// Extended screen: the Windows side runs ffmpeg (gdigrab -> H.264 -> UDP) and
// the Ubuntu side runs ffplay fullscreen. Both are started/stopped over the channel.

import (
	"fmt"
	"log"
	"os/exec"
	"runtime"
	"sync"
)

const screenPort = 5000

type screenState struct {
	mu  sync.Mutex
	cmd *exec.Cmd
	on  bool
}

type screenOpts struct {
	X, Y, W, H, FPS int
}

func (a *app) screenOn() bool {
	a.scr.mu.Lock()
	defer a.scr.mu.Unlock()
	return a.scr.on
}

func (a *app) screenStop() {
	a.scr.mu.Lock()
	c := a.scr.cmd
	a.scr.cmd, a.scr.on = nil, false
	a.scr.mu.Unlock()
	if c != nil && c.Process != nil {
		c.Process.Kill()
		c.Wait()
	}
}

func (a *app) screenRun(c *exec.Cmd) error {
	a.screenStop()
	if err := c.Start(); err != nil {
		return err
	}
	a.scr.mu.Lock()
	a.scr.cmd, a.scr.on = c, true
	a.scr.mu.Unlock()
	go func() {
		c.Wait()
		a.scr.mu.Lock()
		if a.scr.cmd == c {
			a.scr.cmd, a.scr.on = nil, false
		}
		a.scr.mu.Unlock()
	}()
	return nil
}

// screenStart is called on the Windows host.
func (a *app) screenStart(o screenOpts) error {
	if runtime.GOOS != "windows" {
		return fmt.Errorf("the screen is shared from the Windows PC")
	}
	s := a.current()
	_, ip := a.peerInfo()
	if s == nil || ip == "" {
		return fmt.Errorf("connect the laptop first")
	}
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		return fmt.Errorf("ffmpeg not found - install it with: winget install Gyan.FFmpeg")
	}
	if o.W <= 0 {
		o.W = 1920
	}
	if o.H <= 0 {
		o.H = 1080
	}
	if o.FPS <= 0 {
		o.FPS = 30
	}
	if err := s.send(msg{T: "screen-start", Port: screenPort}); err != nil {
		return err
	}
	c := exec.Command("ffmpeg", "-loglevel", "error", "-f", "gdigrab", "-framerate", fmt.Sprint(o.FPS),
		"-offset_x", fmt.Sprint(o.X), "-offset_y", fmt.Sprint(o.Y), "-video_size", fmt.Sprintf("%dx%d", o.W, o.H),
		"-i", "desktop", "-c:v", "libx264", "-preset", "ultrafast", "-tune", "zerolatency", "-pix_fmt", "yuv420p",
		"-f", "mpegts", fmt.Sprintf("udp://%s:%d?pkt_size=1316", ip, screenPort))
	hideWindow(c)
	if err := a.screenRun(c); err != nil {
		return err
	}
	log.Print("Extended screen started")
	return nil
}

func (a *app) screenEnd() {
	a.screenStop()
	if s := a.current(); s != nil {
		s.send(msg{T: "screen-stop"})
	}
}

// screenMessage handles screen messages on the receiving side (Ubuntu).
func (a *app) screenMessage(s *session, m msg) {
	switch m.T {
	case "screen-start":
		if runtime.GOOS == "windows" {
			return
		}
		c := exec.Command("ffplay", "-loglevel", "error", "-fflags", "nobuffer", "-flags", "low_delay", "-framedrop",
			"-probesize", "32", "-analyzeduration", "0", "-fs", "-window_title", "PcsConnector screen",
			fmt.Sprintf("udp://0.0.0.0:%d", m.Port))
		if err := a.screenRun(c); err != nil {
			log.Printf("cannot start ffplay (sudo apt install ffmpeg): %v", err)
			return
		}
		log.Print("Showing the Windows extended screen")
	case "screen-stop":
		a.screenStop()
	}
}
