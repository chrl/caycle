package session

import (
	"math"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"caycle/internal/route"
	"caycle/internal/store"
	"caycle/internal/trainer"
)

// fakeDevice rides at a constant 36 km/h, 250 W, 90 rpm and records commands.
type fakeDevice struct {
	mu     sync.Mutex
	now    time.Time
	grades []float64
	erg    []float64
}

func (f *fakeDevice) Metrics() trainer.Metrics {
	f.mu.Lock()
	defer f.mu.Unlock()
	return trainer.Metrics{Power: 250, Cadence: 90, SpeedKmh: 36, Updated: f.now}
}
func (f *fakeDevice) SetResistance(float64) error { return nil }
func (f *fakeDevice) SetTargetPower(w float64) error {
	f.mu.Lock()
	f.erg = append(f.erg, w)
	f.mu.Unlock()
	return nil
}
func (f *fakeDevice) SetGrade(g float64) error {
	f.mu.Lock()
	f.grades = append(f.grades, g)
	f.mu.Unlock()
	return nil
}

// climb is flat for 1 km then 5 % for 1 km.
func climb(t *testing.T) *route.Route {
	var pts []route.Point
	for i := 0; i <= 200; i++ {
		d := float64(i) * 10
		ele := 0.0
		if d > 1000 {
			ele = (d - 1000) * 0.05
		}
		pts = append(pts, route.Point{Lat: 52 + d/111195, Lon: 13, Ele: ele})
	}
	r, err := route.New("Climb", pts, true)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestRideOnRoute(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	info, err := st.AddRoute(climb(t))
	if err != nil {
		t.Fatal(err)
	}

	dev := &fakeDevice{}
	s := New(Config{
		Store:     st,
		Trainer:   func() (Device, string) { return dev, "connected" },
		HeartRate: func() (int, string) { return 150, "strap" },
	})
	if err := s.SetMode(RouteMode); err == nil {
		t.Error("route mode without a route should fail")
	}
	if err := s.SelectRoute(&info.ID); err != nil {
		t.Fatal(err)
	}
	s.SetValue(50) // 50 % difficulty

	id, err := s.StartRide()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.StartRide(); err == nil {
		t.Error("second StartRide should fail")
	}

	// Ride 150 s at 10 m/s = 1.5 km, 4 ticks per second.
	now := time.Now()
	s.lastTick = now
	for range 600 {
		now = now.Add(tickEvery)
		dev.now = now
		s.tick(now)
	}
	snap := s.Snapshot()
	if !near(snap.Ride.DistanceM, 1500, 1) || !near(snap.Ride.MovingS, 150, 0.01) {
		t.Errorf("distance %.1f moving %.1f", snap.Ride.DistanceM, snap.Ride.MovingS)
	}
	if snap.Route == nil || !near(snap.Route.Grade, 5, 0.1) || !near(snap.Route.PositionM, 1500, 1) {
		t.Fatalf("route state %+v", snap.Route)
	}
	// 50 % difficulty halves the 5 % grade sent to the trainer.
	if !near(snap.Control.AppliedGrade, 2.5, 0.1) {
		t.Errorf("applied grade %.2f", snap.Control.AppliedGrade)
	}
	if *snap.HeartRate != 150 || snap.Ride.AvgHeartRate != 150 || !near(snap.Ride.AvgPower, 250, 0.01) {
		t.Errorf("hr %v avg hr %v avg power %v", *snap.HeartRate, snap.Ride.AvgHeartRate, snap.Ride.AvgPower)
	}

	r, err := s.FinishRide()
	if err != nil {
		t.Fatal(err)
	}
	if r.ID != id || !near(r.MovingS, 150, 1) || r.RouteName == nil || *r.RouteName != "Climb" {
		t.Errorf("finished ride %+v", r)
	}
	if !near(r.AscentM, 25, 1.5) { // 500 m at 5 %
		t.Errorf("ascent %.1f", r.AscentM)
	}
	samples, _ := st.Samples(id)
	last := samples[len(samples)-1]
	if last.Lat == nil || last.Grade == nil || !near(*last.Grade, 5, 0.1) || last.HeartRate != 150 {
		t.Errorf("last sample %+v", last)
	}
	if _, err := s.FinishRide(); err != ErrNotRecording {
		t.Errorf("second finish: %v", err)
	}
	if s.LastRideID() != id {
		t.Errorf("last ride id %d", s.LastRideID())
	}
}

func TestStepAndClamp(t *testing.T) {
	s := New(Config{Trainer: func() (Device, string) { return nil, "searching" }, Resistance: 95})
	s.Step(1)
	s.Step(1)
	if snap := s.Snapshot(); snap.Control.Resistance != 100 {
		t.Errorf("resistance %v", snap.Control.Resistance)
	}
	s.SetMode(ERG)
	s.SetValue(200)
	s.Step(-1)
	s.SetMode(Sim)
	s.SetValue(-50)
	snap := s.Snapshot()
	if snap.Control.Power != 190 || snap.Control.Grade != minGrade || snap.Control.Mode != Sim {
		t.Errorf("control %+v", snap.Control)
	}
	if snap.Power != nil || snap.Trainer.Connected {
		t.Errorf("disconnected trainer should report no power")
	}
}

func near(a, b, tol float64) bool { return math.Abs(a-b) <= tol }
