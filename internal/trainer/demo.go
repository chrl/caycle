package trainer

import (
	"math"
	"math/rand/v2"
	"sync"
	"time"

	"caycle/internal/fec"
)

// Demo is a simulated trainer for trying caycle without a bike. The virtual
// rider pushes ~200 W (or the ERG target) and speed follows a simple road
// physics model, so grades feel right on the dashboard.
type Demo struct {
	mu    sync.Mutex
	mode  string
	value float64
	start time.Time
}

func NewDemo() *Demo { return &Demo{mode: "resistance", start: time.Now()} }

func (d *Demo) set(mode string, v float64) error {
	d.mu.Lock()
	d.mode, d.value = mode, v
	d.mu.Unlock()
	return nil
}

func (d *Demo) SetResistance(p float64) error  { return d.set("resistance", p) }
func (d *Demo) SetTargetPower(w float64) error { return d.set("erg", w) }
func (d *Demo) SetGrade(g float64) error       { return d.set("grade", g) }

// speedFor solves P = (Crr·m·g + m·g·grade + ½·ρ·CdA·v²)·v for v (m/s).
func speedFor(power, gradePct float64) float64 {
	const (
		mass = 85.0
		g    = 9.81
		crr  = 0.004
		rho  = 1.2
		cda  = 0.32
	)
	f := func(v float64) float64 {
		return (crr*mass*g+mass*g*gradePct/100+0.5*rho*cda*v*v)*v - power
	}
	lo, hi := 0.0, 30.0
	for range 50 {
		mid := (lo + hi) / 2
		if f(mid) > 0 {
			hi = mid
		} else {
			lo = mid
		}
	}
	return lo
}

func (d *Demo) Metrics() Metrics {
	d.mu.Lock()
	defer d.mu.Unlock()
	t := time.Since(d.start).Seconds()
	wobble := 8*math.Sin(t/7) + rand.Float64()*10 - 5

	power, grade := 200.0, 0.0
	switch d.mode {
	case "resistance":
		power = 120 + d.value*2.5
	case "erg":
		power = d.value
	case "grade":
		grade = d.value
		power = 200 + max(0, grade)*15 // riders push harder uphill
	}
	power = max(0, power+wobble)
	return Metrics{
		Power:     int(power),
		Cadence:   int(88 + 4*math.Sin(t/11) + rand.Float64()*2),
		SpeedKmh:  speedFor(power, grade) * 3.6,
		State:     fec.StateInUse,
		Updated:   time.Now(),
		HeartRate: int(120 + power/10 + 3*math.Sin(t/13)),
	}
}
