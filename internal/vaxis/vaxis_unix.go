//go:build darwin || freebsd || linux || netbsd || openbsd || zos

package vaxis

import (
	"fmt"
	"os/signal"
	"syscall"
	"time"

	"git.sr.ht/~rockorager/vaxis/log"
	"golang.org/x/sys/unix"
)

func (vx *Vaxis) setupSignals() {
	if !vx.caps.inBandResize {
		signal.Notify(vx.chSigWinSz,
			syscall.SIGWINCH,
		)
	}
	signal.Notify(vx.chSigKill,
		// kill signals
		syscall.SIGABRT,
		syscall.SIGBUS,
		syscall.SIGFPE,
		syscall.SIGILL,
		syscall.SIGINT,
		syscall.SIGQUIT,
		syscall.SIGSEGV,
		syscall.SIGTERM,
	)
}

// reportWinsize
func (vx *Vaxis) drainPendingInput() {
	fd := int(vx.console.Fd())
	flags, err := unix.FcntlInt(uintptr(fd), unix.F_GETFL, 0)
	if err != nil {
		return
	}
	wasNonblock := flags&unix.O_NONBLOCK != 0
	if !wasNonblock {
		if err := unix.SetNonblock(fd, true); err != nil {
			return
		}
		defer func() { _, _ = unix.FcntlInt(uintptr(fd), unix.F_SETFL, flags) }()
	}

	buf := make([]byte, 64)
	deadline := time.Now().Add(20 * time.Millisecond)
	for {
		_, err := unix.Read(fd, buf)
		if err == unix.EAGAIN || err == unix.EWOULDBLOCK {
			if time.Now().After(deadline) {
				return
			}
			time.Sleep(time.Millisecond)
			continue
		}
		if err != nil {
			return
		}
	}
}

func (vx *Vaxis) reportWinsize() (Resize, error) {
	if vx.caps.inBandResize {
		// We already received the size if we have in band reports
		vx.mu.Lock()
		defer vx.mu.Unlock()
		return vx.nextSize, nil
	}
	if vx.xtwinops && vx.caps.reportSizeChars && vx.caps.reportSizePixels {
		log.Trace("requesting screen size from terminal")
		vx.writeControlString(textAreaSize)
		deadline := time.NewTimer(100 * time.Millisecond)
		select {
		case <-deadline.C:
			return Resize{}, fmt.Errorf("screen size request deadline exceeded")
		case <-vx.chSizeDone:
			vx.mu.Lock()
			defer vx.mu.Unlock()
			return vx.nextSize, nil
		}
	}
	log.Trace("requesting screen size from ioctl")
	ws, err := unix.IoctlGetWinsize(int(vx.console.Fd()), unix.TIOCGWINSZ)
	if err != nil {
		cws, err := vx.console.Size()
		if err == nil {
			return Resize{
				Cols: int(cws.Width),
				Rows: int(cws.Height),
			}, nil
		}

		return Resize{}, err
	}
	return Resize{
		Cols:   int(ws.Col),
		Rows:   int(ws.Row),
		XPixel: int(ws.Xpixel),
		YPixel: int(ws.Ypixel),
	}, nil
}
