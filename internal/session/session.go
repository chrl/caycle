// Package session is the heart of a ride: it reads the trainer and heart rate
// sensor, applies the chosen mode (resistance, ERG, grade or route), follows a
// route and records the ride to the store. The terminal and web UIs are thin
// layers over it.
package session

import (
	"errors"
	"fmt"
	"log"
	"math"
	"sync"
	"time"

	"caycle/internal/fec"
	"caycle/internal/route"
	"caycle/internal/store"
	"caycle/internal/trainer"
)

// Mode is how the trainer resistance is controlled.
type Mode string

const (
	Resistance Mode = "resistance" // fixed brake level, %
	ERG        Mode = "erg"        // target power, W
	Sim        Mode = "sim"        // manual road grade, %
	RouteMode  Mode = "route"      // grade follows the selected route
)

// Device is a controllable trainer (real or simulated).
type Device interface {
	Metrics() trainer.Metrics
	SetResistance(percent float64) error
	SetTargetPower(watts float64) error
	SetGrade(percent float64) error
}

// Config wires a session to its trainer, heart rate sensor and store.
type Config struct {
	Store *store.Store
	// Trainer returns the connected trainer (nil while disconnected) and a status line.
	Trainer func() (Device, string)
	// HeartRate returns the external sensor's bpm (0 if none) and a status line.
	// Nil when no external sensor is used.
	HeartRate func() (int, string)

	Mode       Mode
	Resistance float64
	Power      float64
	Grade      float64
	Difficulty float64 // % of the route grade applied in route mode
}

const (
	minGrade, maxGrade = -10.0, 20.0
	tickEvery          = 250 * time.Millisecond
	resendEvery        = 5 * time.Second
)

// Session is safe for concurrent use.
type Session struct {
	cfg     Config
	changed chan struct{}

	mu         sync.Mutex
	mode       Mode
	resistance float64
	power      float64
	grade      float64
	difficulty float64
	applied    float64 // grade sent in route mode
	writeErr   string

	dev       Device
	devStatus string
	metrics   trainer.Metrics
	hrStatus  string
	lastTick  time.Time

	// current ride; rideID == 0 when not recording
	rideID     int64
	lastSample time.Time
	moving     time.Duration
	distance   float64
	energyJ    float64
	maxPower   int
	hrSum      float64
	hrN        int
	ascent     float64
	lastEle    *float64
	lastRideID int64

	// selected route
	rt          *route.Route
	routeID     int64
	routeOffset float64 // ride distance where the route started
}

// New creates a session; call Run to start it.
func New(cfg Config) *Session {
	if cfg.Mode == "" {
		cfg.Mode = Resistance
	}
	if cfg.Difficulty == 0 {
		cfg.Difficulty = 100
	}
	return &Session{
		cfg:        cfg,
		changed:    make(chan struct{}, 1),
		mode:       cfg.Mode,
		resistance: cfg.Resistance,
		power:      cfg.Power,
		grade:      cfg.Grade,
		difficulty: cfg.Difficulty,
		metrics:    trainer.Metrics{Power: -1, Cadence: -1},
	}
}

func (s *Session) notify() {
	select {
	case s.changed <- struct{}{}:
	default:
	}
}

// Run drives the session until stop is closed.
func (s *Session) Run(stop <-chan struct{}) {
	go s.sendLoop(stop)
	tick := time.NewTicker(tickEvery)
	defer tick.Stop()
	s.mu.Lock()
	s.lastTick = time.Now()
	s.mu.Unlock()
	s.tick(time.Now())
	for {
		select {
		case <-stop:
			return
		case now := <-tick.C:
			s.tick(now)
		}
	}
}

// sendLoop pushes the control setting to the trainer when it changes, and
// re-sends it periodically in case a write was lost.
func (s *Session) sendLoop(stop <-chan struct{}) {
	resend := time.NewTicker(resendEvery)
	defer resend.Stop()
	for {
		select {
		case <-stop:
			return
		case <-s.changed:
		case <-resend.C:
		}
		s.mu.Lock()
		dev, mode := s.dev, s.mode
		var v float64
		switch mode {
		case Resistance:
			v = s.resistance
		case ERG:
			v = s.power
		case Sim:
			v = s.grade
		case RouteMode:
			v = s.applied
		}
		s.mu.Unlock()
		if dev == nil {
			continue
		}
		var err error
		switch mode {
		case Resistance:
			err = dev.SetResistance(v)
		case ERG:
			err = dev.SetTargetPower(v)
		default:
			err = dev.SetGrade(v)
		}
		s.mu.Lock()
		s.writeErr = ""
		if err != nil {
			s.writeErr = "write failed: " + err.Error()
		}
		s.mu.Unlock()
	}
}

func isMoving(m trainer.Metrics) bool {
	return m.Cadence > 0 || m.Power > 0 || m.SpeedKmh >= 1
}

func (s *Session) tick(now time.Time) {
	dev, devStatus := s.cfg.Trainer()
	m := trainer.Metrics{Power: -1, Cadence: -1}
	if dev != nil {
		m = dev.Metrics()
		if m.Updated.IsZero() || now.Sub(m.Updated) > 3*time.Second {
			devStatus = "no data, pedal to wake the trainer"
			m.Power, m.Cadence, m.SpeedKmh = -1, -1, 0
		}
	}
	hrStatus := "off"
	if s.cfg.HeartRate != nil {
		var bpm int
		bpm, hrStatus = s.cfg.HeartRate()
		if bpm > 0 {
			m.HeartRate = bpm
		}
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if dev != s.dev {
		s.notify() // (re)connected: send the current setting right away
	}
	s.dev, s.devStatus, s.metrics, s.hrStatus = dev, devStatus, m, hrStatus
	dt := now.Sub(s.lastTick)
	s.lastTick = now

	moving := s.rideID != 0 && isMoving(m)
	if moving {
		s.moving += dt
		s.distance += m.SpeedKmh / 3.6 * dt.Seconds()
		if m.Power > 0 {
			s.energyJ += float64(m.Power) * dt.Seconds()
		}
	}

	if s.rt != nil && s.mode == RouteMode {
		g := 0.0
		if pos := s.distance - s.routeOffset; pos < s.rt.Distance() {
			g = s.rt.Grade(pos) * s.difficulty / 100
		}
		g = max(minGrade, min(maxGrade, g))
		if math.Abs(g-s.applied) >= 0.1 {
			s.applied = g
			s.notify()
		}
	}

	if moving && now.Sub(s.lastSample) >= time.Second {
		s.lastSample = now
		s.recordSample(now, m)
	}
}

// recordSample stores one second of data. Called with s.mu held.
func (s *Session) recordSample(now time.Time, m trainer.Metrics) {
	x := store.Sample{
		T: now, Power: m.Power, Cadence: m.Cadence, HeartRate: m.HeartRate,
		SpeedKmh: m.SpeedKmh, DistanceM: s.distance, Mode: string(s.mode), Target: s.targetLocked(),
	}
	if s.rt != nil {
		pos := s.distance - s.routeOffset
		lat, lon, ele := s.rt.At(pos)
		grade := s.rt.Grade(pos)
		x.Lat, x.Lon, x.Ele, x.Grade = &lat, &lon, &ele, &grade
		if s.lastEle != nil {
			s.ascent += max(0, ele-*s.lastEle)
		}
		s.lastEle = &ele
	}
	if m.Power > s.maxPower {
		s.maxPower = m.Power
	}
	if m.HeartRate > 0 {
		s.hrSum += float64(m.HeartRate)
		s.hrN++
	}
	if err := s.cfg.Store.AddSample(s.rideID, x); err != nil {
		log.Printf("save sample: %v", err)
	}
}

func (s *Session) targetLocked() float64 {
	switch s.mode {
	case ERG:
		return s.power
	case Sim:
		return s.grade
	case RouteMode:
		return s.applied
	}
	return s.resistance
}

// SetMode switches the control mode.
func (s *Session) SetMode(m Mode) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	switch m {
	case Resistance, ERG, Sim:
	case RouteMode:
		if s.rt == nil {
			return errors.New("select a route first")
		}
	default:
		return fmt.Errorf("unknown mode %q", m)
	}
	s.mode = m
	s.notify()
	return nil
}

func clampTarget(m Mode, v float64) float64 {
	switch m {
	case Resistance:
		return max(0, min(100, v))
	case ERG:
		return max(0, min(1500, v))
	case Sim:
		return max(minGrade, min(maxGrade, v))
	default:
		return max(0, min(100, v))
	}
}

// SetValue sets the current mode's target: resistance %, power W, grade %
// or route difficulty %.
func (s *Session) SetValue(v float64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	v = clampTarget(s.mode, v)
	switch s.mode {
	case Resistance:
		s.resistance = v
	case ERG:
		s.power = v
	case Sim:
		s.grade = v
	case RouteMode:
		s.difficulty = v
	}
	s.notify()
}

// Step nudges the current target up (dir > 0) or down.
func (s *Session) Step(dir int) {
	s.mu.Lock()
	step, cur := 0.0, 0.0
	switch s.mode {
	case Resistance:
		step, cur = 5, s.resistance
	case ERG:
		step, cur = 10, s.power
	case Sim:
		step, cur = 0.5, s.grade
	case RouteMode:
		step, cur = 10, s.difficulty
	}
	s.mu.Unlock()
	if dir < 0 {
		step = -step
	}
	s.SetValue(cur + step)
}

// SelectRoute picks a stored route (nil clears it) and switches to route mode.
func (s *Session) SelectRoute(id *int64) error {
	if id == nil {
		s.mu.Lock()
		defer s.mu.Unlock()
		s.rt, s.routeID, s.lastEle = nil, 0, nil
		if s.mode == RouteMode {
			s.mode, s.grade = Sim, 0
		}
		if s.rideID != 0 {
			s.cfg.Store.SetRideRoute(s.rideID, nil)
		}
		s.notify()
		return nil
	}
	_, rt, err := s.cfg.Store.Route(*id)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.rt, s.routeID, s.lastEle = rt, *id, nil
	s.routeOffset = s.distance
	s.mode, s.applied = RouteMode, 0
	if s.rideID != 0 {
		if err := s.cfg.Store.SetRideRoute(s.rideID, id); err != nil {
			return err
		}
	}
	s.notify()
	return nil
}

// StartRide starts recording a new ride.
func (s *Session) StartRide() (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.rideID != 0 {
		return 0, errors.New("a ride is already being recorded")
	}
	id, err := s.cfg.Store.StartRide(time.Now())
	if err != nil {
		return 0, err
	}
	if s.rt != nil {
		if err := s.cfg.Store.SetRideRoute(id, &s.routeID); err != nil {
			return 0, err
		}
	}
	s.rideID = id
	s.moving, s.distance, s.energyJ, s.maxPower = 0, 0, 0, 0
	s.hrSum, s.hrN, s.ascent, s.lastEle = 0, 0, 0, nil
	s.routeOffset = 0
	return id, nil
}

// ErrNotRecording is returned by FinishRide when no ride is in progress.
var ErrNotRecording = errors.New("no ride is being recorded")

// FinishRide stops recording and returns the saved ride with its totals.
func (s *Session) FinishRide() (store.Ride, error) {
	s.mu.Lock()
	id := s.rideID
	s.rideID = 0
	if id != 0 {
		s.lastRideID = id
	}
	s.mu.Unlock()
	if id == 0 {
		return store.Ride{}, ErrNotRecording
	}
	return s.cfg.Store.FinishRide(id, time.Now())
}

// LastRideID is the most recently finished ride in this session (0 if none).
func (s *Session) LastRideID() int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.lastRideID
}

// Snapshot is the live state, shaped for the web UI.
type Snapshot struct {
	Trainer struct {
		Connected    bool   `json:"connected"`
		Name         string `json:"name"`
		State        string `json:"state"`
		TargetStatus string `json:"targetStatus"`
		WriteError   string `json:"writeError"`
	} `json:"trainer"`
	HR struct {
		Status string `json:"status"`
	} `json:"hr"`
	Power     *int    `json:"power"`
	Cadence   *int    `json:"cadence"`
	HeartRate *int    `json:"heartRate"`
	SpeedKmh  float64 `json:"speedKmh"`
	Control   struct {
		Mode         Mode    `json:"mode"`
		Resistance   float64 `json:"resistance"`
		Power        float64 `json:"power"`
		Grade        float64 `json:"grade"`
		Difficulty   float64 `json:"difficulty"`
		AppliedGrade float64 `json:"appliedGrade"`
	} `json:"control"`
	Ride struct {
		Recording    bool    `json:"recording"`
		ID           int64   `json:"id"`
		MovingS      float64 `json:"movingS"`
		DistanceM    float64 `json:"distanceM"`
		AvgPower     float64 `json:"avgPower"`
		MaxPower     int     `json:"maxPower"`
		EnergyKJ     float64 `json:"energyKJ"`
		AscentM      float64 `json:"ascentM"`
		AvgHeartRate float64 `json:"avgHeartRate"`
	} `json:"ride"`
	Route  *RouteState `json:"route"`
	Strava struct {
		Connected bool `json:"connected"`
	} `json:"strava"`
}

// RouteState is the rider's position on the selected route.
type RouteState struct {
	ID        int64   `json:"id"`
	Name      string  `json:"name"`
	DistanceM float64 `json:"distanceM"`
	PositionM float64 `json:"positionM"`
	Lat       float64 `json:"lat"`
	Lon       float64 `json:"lon"`
	EleM      float64 `json:"eleM"`
	Grade     float64 `json:"grade"`
	Done      bool    `json:"done"`
}

func intPtr(v int) *int {
	if v < 0 {
		return nil
	}
	return &v
}

// Snapshot returns the current live state.
func (s *Session) Snapshot() Snapshot {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out Snapshot
	m := s.metrics
	out.Trainer.Connected = s.dev != nil
	out.Trainer.Name = s.devStatus
	if t, ok := s.dev.(*trainer.Trainer); ok && t.Name != "" && s.devStatus == "connected" {
		out.Trainer.Name = t.Name
	}
	if _, ok := s.dev.(*trainer.Demo); ok {
		out.Trainer.Name = "Demo trainer"
	}
	out.Trainer.State = m.State.String()
	if s.mode == ERG && s.dev != nil && m.TargetStatus != fec.TargetOK {
		out.Trainer.TargetStatus = m.TargetStatus.String()
	}
	out.Trainer.WriteError = s.writeErr
	out.HR.Status = s.hrStatus

	out.Power, out.Cadence = intPtr(m.Power), intPtr(m.Cadence)
	if m.HeartRate > 0 {
		out.HeartRate = &m.HeartRate
	}
	out.SpeedKmh = m.SpeedKmh

	c := &out.Control
	c.Mode, c.Resistance, c.Power, c.Grade, c.Difficulty, c.AppliedGrade =
		s.mode, s.resistance, s.power, s.grade, s.difficulty, s.applied

	r := &out.Ride
	r.Recording, r.ID = s.rideID != 0, s.rideID
	r.MovingS, r.DistanceM, r.MaxPower = s.moving.Seconds(), s.distance, s.maxPower
	r.EnergyKJ, r.AscentM = s.energyJ/1000, s.ascent
	if s.moving > 0 {
		r.AvgPower = s.energyJ / s.moving.Seconds()
	}
	if s.hrN > 0 {
		r.AvgHeartRate = s.hrSum / float64(s.hrN)
	}

	if s.rt != nil {
		pos := max(0, min(s.distance-s.routeOffset, s.rt.Distance()))
		lat, lon, ele := s.rt.At(pos)
		out.Route = &RouteState{
			ID: s.routeID, Name: s.rt.Name, DistanceM: s.rt.Distance(), PositionM: pos,
			Lat: lat, Lon: lon, EleM: ele, Grade: s.rt.Grade(pos), Done: pos >= s.rt.Distance(),
		}
	}
	return out
}
