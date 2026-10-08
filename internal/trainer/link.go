package trainer

import (
	"sync"
	"time"

	"caycle/internal/ble"

	"tinygo.org/x/bluetooth"
)

// Link keeps a trainer connected in the background, reconnecting when it
// drops, so a ride survives the trainer going to sleep.
type Link struct {
	mu     sync.Mutex
	t      *Trainer
	status string
}

// Current returns the connected trainer (nil if none) and a status line.
func (l *Link) Current() (*Trainer, string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.t, l.status
}

func (l *Link) set(t *Trainer, status string) {
	l.mu.Lock()
	l.t, l.status = t, status
	l.mu.Unlock()
}

// Keep starts connecting to the trainer matching query (see MatchDevice).
// onConnect runs after every (re)connection, e.g. to send user settings.
func Keep(adapter *bluetooth.Adapter, query string, onConnect func(*Trainer), stop <-chan struct{}) *Link {
	l := &Link{status: "searching for trainer..."}
	go func() {
		for {
			r, err := ble.Find(adapter, 15*time.Second, MatchDevice(query))
			if err == nil {
				l.set(nil, "connecting to "+r.LocalName()+"...")
				var t *Trainer
				if t, err = Connect(adapter, r); err == nil {
					if onConnect != nil {
						onConnect(t)
					}
					l.set(t, "connected")
					select {
					case <-t.Done():
						l.set(nil, "trainer disconnected, reconnecting...")
					case <-stop:
						t.Close()
						return
					}
					continue
				}
			}
			l.set(nil, "trainer not found, retrying... (pedal to wake it, close other apps)")
			select {
			case <-stop:
				return
			case <-time.After(2 * time.Second):
			}
		}
	}()
	return l
}
