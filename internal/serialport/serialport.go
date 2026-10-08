// Package serialport talks line-based text to a USB serial device, such as
// the caycle ESP32 board's console.
package serialport

import (
	"bufio"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/sys/unix"
)

// Port is an open serial port at 115200 baud, 8N1, raw.
type Port struct {
	f   *os.File
	buf *bufio.Reader
}

// Find returns the first USB serial device (CP210x / CH34x / FTDI style).
func Find() (string, error) {
	for _, pattern := range []string{"/dev/cu.usbserial-*", "/dev/cu.SLAB_USBtoUART*", "/dev/cu.wchusbserial*", "/dev/ttyUSB*", "/dev/ttyACM*"} {
		if m, _ := filepath.Glob(pattern); len(m) > 0 {
			return m[0], nil
		}
	}
	return "", errors.New("no USB serial device found; is the board plugged in?")
}

// Open opens and configures the port. Reads return after at most 100 ms, so
// callers can implement timeouts with a simple loop.
func Open(path string) (*Port, error) {
	fd, err := unix.Open(path, unix.O_RDWR|unix.O_NOCTTY|unix.O_NONBLOCK, 0)
	if err != nil {
		return nil, err
	}
	if err := configure(fd); err != nil {
		unix.Close(fd)
		return nil, err
	}
	if err := unix.SetNonblock(fd, false); err != nil {
		unix.Close(fd)
		return nil, err
	}
	f := os.NewFile(uintptr(fd), path)
	return &Port{f: f, buf: bufio.NewReader(f)}, nil
}

func (p *Port) Close() error { return p.f.Close() }

// WriteLine sends a line terminated by "\n".
func (p *Port) WriteLine(s string) error {
	_, err := p.f.WriteString(s + "\n")
	return err
}

// ReadLine returns the next line, or ok=false if none arrived before the deadline.
func (p *Port) ReadLine(deadline time.Time) (line string, ok bool) {
	var sb strings.Builder
	for time.Now().Before(deadline) {
		b, err := p.buf.ReadByte()
		if err != nil { // the 100 ms read timeout returns EOF
			continue
		}
		if b == '\n' {
			return strings.TrimRight(sb.String(), "\r"), true
		}
		sb.WriteByte(b)
	}
	return sb.String(), false
}
