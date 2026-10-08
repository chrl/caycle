// Package trainer talks to a Tacx smart trainer over Bluetooth LE using the
// Tacx "FE-C over BLE" service.
package trainer

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"caycle/internal/ble"
	"caycle/internal/fec"

	"tinygo.org/x/bluetooth"
)

var (
	FECService = mustUUID("6e40fec1-b5a3-f393-e0a9-e50e24dcca9e")
	fecNotify  = mustUUID("6e40fec2-b5a3-f393-e0a9-e50e24dcca9e")
	fecWrite   = mustUUID("6e40fec3-b5a3-f393-e0a9-e50e24dcca9e")

	FTMSService         = bluetooth.New16BitUUID(0x1826)
	CyclingPowerService = bluetooth.New16BitUUID(0x1818)
	CyclingSpeedCadence = bluetooth.New16BitUUID(0x1816)
)

func mustUUID(s string) bluetooth.UUID {
	u, err := bluetooth.ParseUUID(s)
	if err != nil {
		panic(err)
	}
	return u
}

// Metrics is the latest state reported by the trainer.
type Metrics struct {
	Power        int // W, -1 if unknown
	Cadence      int // rpm, -1 if unknown
	SpeedKmh     float64
	HeartRate    int     // bpm, 0 if unknown
	DistanceM    float64 // accumulated since connect
	State        fec.FEState
	TargetStatus fec.TargetPowerStatus
	Updated      time.Time
}

// Trainer is a connected FE-C over BLE trainer.
type Trainer struct {
	Name string

	dev   bluetooth.Device
	write bluetooth.DeviceCharacteristic
	done  chan struct{}

	writeMu sync.Mutex
	noResp  bool // trainer rejected write-with-response; use write-without-response

	mu       sync.Mutex
	m        Metrics
	lastDist uint8
	haveDist bool
}

// LooksLikeTrainer reports whether an advertisement is probably a smart trainer.
func LooksLikeTrainer(r bluetooth.ScanResult) bool {
	return strings.Contains(strings.ToLower(r.LocalName()), "tacx") ||
		r.HasServiceUUID(FECService) ||
		r.HasServiceUUID(FTMSService)
}

// MatchDevice returns a matcher for a user-supplied name substring or address.
// An empty query matches the first device that looks like a trainer.
func MatchDevice(query string) func(bluetooth.ScanResult) bool {
	q := strings.ToLower(query)
	return func(r bluetooth.ScanResult) bool {
		if q == "" {
			return LooksLikeTrainer(r)
		}
		return strings.EqualFold(r.Address.String(), query) ||
			(r.LocalName() != "" && strings.Contains(strings.ToLower(r.LocalName()), q))
	}
}

// Connect connects to a scanned device and subscribes to FE-C data.
func Connect(adapter *bluetooth.Adapter, r bluetooth.ScanResult) (*Trainer, error) {
	t := &Trainer{
		Name: r.LocalName(),
		done: make(chan struct{}),
		m:    Metrics{Power: -1, Cadence: -1},
	}
	ble.OnDisconnect(adapter, r.Address, func() { close(t.done) })

	dev, err := adapter.Connect(r.Address, bluetooth.ConnectionParams{})
	if err != nil {
		return nil, fmt.Errorf("connect: %w", err)
	}
	t.dev = dev

	svcs, err := dev.DiscoverServices([]bluetooth.UUID{FECService})
	if err != nil || len(svcs) == 0 {
		dev.Disconnect()
		return nil, errors.New("trainer does not expose the Tacx FE-C over BLE service; run `caycle inspect` to see what it offers")
	}
	chars, err := svcs[0].DiscoverCharacteristics([]bluetooth.UUID{fecNotify, fecWrite})
	if err != nil {
		dev.Disconnect()
		return nil, fmt.Errorf("discover characteristics: %w", err)
	}
	var notify *bluetooth.DeviceCharacteristic
	for i := range chars {
		switch chars[i].UUID() {
		case fecNotify:
			notify = &chars[i]
		case fecWrite:
			t.write = chars[i]
		}
	}
	if notify == nil || t.write.UUID() != fecWrite {
		dev.Disconnect()
		return nil, errors.New("FE-C service is missing its notify or write characteristic")
	}
	if err := notify.EnableNotifications(t.handle); err != nil {
		dev.Disconnect()
		return nil, fmt.Errorf("enable notifications: %w", err)
	}
	return t, nil
}

// Done is closed when the trainer disconnects.
func (t *Trainer) Done() <-chan struct{} { return t.done }

// Close disconnects from the trainer.
func (t *Trainer) Close() error { return t.dev.Disconnect() }

// Metrics returns a snapshot of the latest trainer data.
func (t *Trainer) Metrics() Metrics {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.m
}

// handle processes a notification, which may hold one or more ANT frames.
func (t *Trainer) handle(buf []byte) {
	for len(buf) >= 13 {
		page, err := fec.Decode(buf[:13])
		if err != nil {
			// Resynchronise on the next sync byte.
			buf = buf[1:]
			continue
		}
		buf = buf[13:]
		t.apply(page)
	}
}

func (t *Trainer) apply(page [8]byte) {
	t.mu.Lock()
	defer t.mu.Unlock()
	switch page[0] {
	case fec.PageGeneralFE:
		g := fec.ParseGeneralFE(page)
		t.m.SpeedKmh = g.SpeedMps * 3.6
		t.m.HeartRate = g.HeartRate
		t.m.State = g.State
		if t.haveDist {
			t.m.DistanceM += float64(g.DistanceM - t.lastDist) // uint8 arithmetic handles rollover
		}
		t.lastDist, t.haveDist = g.DistanceM, true
	case fec.PageTrainerData:
		d := fec.ParseTrainerData(page)
		t.m.Power = d.Power
		t.m.Cadence = d.Cadence
		t.m.State = d.State
		t.m.TargetStatus = d.TargetStatus
	default:
		return
	}
	t.m.Updated = time.Now()
}

func (t *Trainer) send(page [8]byte) error {
	t.writeMu.Lock()
	defer t.writeMu.Unlock()
	frame := fec.Encode(page)
	if !t.noResp {
		if _, err := t.write.Write(frame); err == nil {
			return nil
		}
		t.noResp = true
	}
	_, err := t.write.WriteWithoutResponse(frame)
	return err
}

// SetResistance sets basic resistance as a percentage (0-100) of the maximum.
func (t *Trainer) SetResistance(percent float64) error {
	return t.send(fec.BasicResistance(percent))
}

// SetTargetPower switches to ERG mode with the given target in watts.
func (t *Trainer) SetTargetPower(watts float64) error {
	return t.send(fec.TargetPower(watts))
}

// SetGrade switches to simulation mode with the given road grade in percent.
func (t *Trainer) SetGrade(percent float64) error {
	return t.send(fec.TrackResistance(percent))
}

// SetUserConfig sends rider weight, bike weight (kg) and wheel diameter (m).
func (t *Trainer) SetUserConfig(userKg, bikeKg, wheelM float64) error {
	return t.send(fec.UserConfig(userKg, bikeKg, wheelM))
}
