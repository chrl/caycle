// Package hrm reads a standard Bluetooth heart rate sensor (Heart Rate
// Service 0x180D): a chest strap, or an iPhone app relaying Apple Watch data.
package hrm

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"caycle/internal/ble"

	"tinygo.org/x/bluetooth"
)

var (
	Service     = bluetooth.New16BitUUID(0x180D)
	measurement = bluetooth.New16BitUUID(0x2A37)
)

// staleAfter is how long a reading stays valid without a new notification.
const staleAfter = 5 * time.Second

// Parse decodes a Heart Rate Measurement characteristic value.
func Parse(b []byte) (bpm int, ok bool) {
	if len(b) < 2 {
		return 0, false
	}
	if b[0]&0x01 == 0 {
		return int(b[1]), true
	}
	if len(b) < 3 {
		return 0, false
	}
	return int(uint16(b[1]) | uint16(b[2])<<8), true
}

// Match returns a matcher for a name substring or address; an empty query
// matches the first device advertising the heart rate service.
func Match(query string) func(bluetooth.ScanResult) bool {
	q := strings.ToLower(query)
	return func(r bluetooth.ScanResult) bool {
		if q == "" {
			return r.HasServiceUUID(Service)
		}
		return strings.EqualFold(r.Address.String(), query) ||
			(r.LocalName() != "" && strings.Contains(strings.ToLower(r.LocalName()), q))
	}
}

// Link keeps a heart rate sensor connected in the background, reconnecting
// when it drops.
type Link struct {
	mu      sync.Mutex
	name    string
	bpm     int
	updated time.Time
	status  string
}

// Reading returns the current heart rate (0 if none) and a status line.
func (l *Link) Reading() (int, string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.bpm > 0 && time.Since(l.updated) < staleAfter {
		return l.bpm, l.name
	}
	return 0, l.status
}

func (l *Link) setStatus(s string) {
	l.mu.Lock()
	l.status = s
	l.mu.Unlock()
}

func (l *Link) handle(buf []byte) {
	if bpm, ok := Parse(buf); ok {
		l.mu.Lock()
		l.bpm, l.updated = bpm, time.Now()
		l.mu.Unlock()
	}
}

// Run starts connecting to a sensor in the background until stop is closed.
func Run(adapter *bluetooth.Adapter, query string, stop <-chan struct{}) *Link {
	l := &Link{status: "searching..."}
	go func() {
		for {
			err := l.session(adapter, query, stop)
			select {
			case <-stop:
				return
			default:
			}
			l.setStatus(err.Error() + ", retrying...")
			select {
			case <-stop:
				return
			case <-time.After(10 * time.Second):
			}
			l.setStatus("searching...")
		}
	}()
	return l
}

// session finds, connects and stays connected until the sensor drops or stop
// is closed.
func (l *Link) session(adapter *bluetooth.Adapter, query string, stop <-chan struct{}) error {
	r, err := ble.Find(adapter, 10*time.Second, Match(query))
	if err != nil {
		return errors.New("no heart rate sensor found")
	}
	name := r.LocalName()
	if name == "" {
		name = r.Address.String()
	}
	l.setStatus("connecting to " + name + "...")

	lost := make(chan struct{})
	ble.OnDisconnect(adapter, r.Address, func() { close(lost) })
	dev, err := adapter.Connect(r.Address, bluetooth.ConnectionParams{})
	if err != nil {
		return fmt.Errorf("connect %s failed", name)
	}
	defer dev.Disconnect()

	svcs, err := dev.DiscoverServices([]bluetooth.UUID{Service})
	if err != nil || len(svcs) == 0 {
		return fmt.Errorf("%s has no heart rate service", name)
	}
	chars, err := svcs[0].DiscoverCharacteristics([]bluetooth.UUID{measurement})
	if err != nil || len(chars) == 0 {
		return fmt.Errorf("%s has no heart rate measurement", name)
	}
	if err := chars[0].EnableNotifications(l.handle); err != nil {
		return fmt.Errorf("%s: enable notifications failed", name)
	}
	l.mu.Lock()
	l.name, l.status = name, name+" (waiting for data)"
	l.mu.Unlock()

	select {
	case <-lost:
		return fmt.Errorf("%s disconnected", name)
	case <-stop:
		return nil
	}
}
