package store

import (
	"path/filepath"
	"testing"
	"time"

	"caycle/internal/route"
)

func open(t *testing.T) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "caycle.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func testRoute(t *testing.T) *route.Route {
	t.Helper()
	r, err := route.New("Hill", []route.Point{
		{Lat: 52, Lon: 13, Ele: 10},
		{Lat: 52.005, Lon: 13, Ele: 20},
		{Lat: 52.01, Lon: 13, Ele: 40},
	}, true)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestRoutes(t *testing.T) {
	s := open(t)
	info, err := s.AddRoute(testRoute(t))
	if err != nil {
		t.Fatal(err)
	}
	list, err := s.Routes()
	if err != nil || len(list) != 1 || list[0].Name != "Hill" || list[0].ID != info.ID {
		t.Fatalf("routes = %+v, %v", list, err)
	}
	_, r, err := s.Route(info.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Points) != 3 || !r.HasEle || r.Distance() < 1000 {
		t.Errorf("loaded route: %d points, hasEle %v, %.0f m", len(r.Points), r.HasEle, r.Distance())
	}
	if err := s.DeleteRoute(info.ID); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.Route(info.ID); err != ErrNotFound {
		t.Errorf("after delete: %v", err)
	}
	if err := s.DeleteRoute(info.ID); err != ErrNotFound {
		t.Errorf("double delete: %v", err)
	}
}

func TestRideLifecycle(t *testing.T) {
	s := open(t)
	routeInfo, err := s.AddRoute(testRoute(t))
	if err != nil {
		t.Fatal(err)
	}
	start := time.Date(2026, 10, 8, 18, 0, 0, 0, time.UTC)
	id, err := s.StartRide(start)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetRideRoute(id, &routeInfo.ID); err != nil {
		t.Fatal(err)
	}
	samples := []Sample{
		{T: start, Power: 100, Cadence: 80, HeartRate: 0, SpeedKmh: 25, DistanceM: 7, Ele: new(float64(10)), Mode: "route"},
		{T: start.Add(time.Second), Power: 200, Cadence: 90, HeartRate: 120, SpeedKmh: 26, DistanceM: 14, Ele: new(float64(12)), Mode: "route"},
		{T: start.Add(2 * time.Second), Power: -1, Cadence: -1, HeartRate: 140, SpeedKmh: 26, DistanceM: 21, Ele: new(float64(11)), Mode: "route"},
		{T: start.Add(3 * time.Second), Power: 300, Cadence: 100, HeartRate: 160, SpeedKmh: 27, DistanceM: 28, Ele: new(float64(14)), Mode: "route"},
	}
	for _, x := range samples {
		if err := s.AddSample(id, x); err != nil {
			t.Fatal(err)
		}
	}
	r, err := s.FinishRide(id, start.Add(5*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if r.MovingS != 4 || r.DistanceM != 28 || r.MaxPower != 300 || r.MaxHeartRate != 160 {
		t.Errorf("totals: %+v", r)
	}
	// Unknown power (-1) counts as zero; HR and cadence averages skip zeros.
	if r.AvgPower != 150 || r.AvgHeartRate != 140 || r.AvgCadence != 90 || r.EnergyKJ != 0.6 {
		t.Errorf("averages: power %v hr %v cadence %v kJ %v", r.AvgPower, r.AvgHeartRate, r.AvgCadence, r.EnergyKJ)
	}
	if r.AscentM != 5 { // +2, -1, +3
		t.Errorf("ascent = %v", r.AscentM)
	}
	if r.RouteName == nil || *r.RouteName != "Hill" || r.EndedAt == nil {
		t.Errorf("route/end: %+v", r)
	}

	got, err := s.Samples(id)
	if err != nil || len(got) != 4 || got[1].TMS != start.Add(time.Second).UnixMilli() || *got[3].Ele != 14 || got[0].Lat != nil {
		t.Fatalf("samples = %+v, %v", got, err)
	}

	if err := s.SetStravaURL(id, "https://www.strava.com/activities/1"); err != nil {
		t.Fatal(err)
	}
	rides, err := s.Rides()
	if err != nil || len(rides) != 1 || *rides[0].StravaURL != "https://www.strava.com/activities/1" {
		t.Fatalf("rides = %+v, %v", rides, err)
	}

	// Deleting the route keeps the ride.
	if err := s.DeleteRoute(routeInfo.ID); err != nil {
		t.Fatal(err)
	}
	if r, err := s.Ride(id); err != nil || r.RouteName != nil {
		t.Errorf("ride after route delete: %+v, %v", r, err)
	}

	if err := s.DeleteRide(id); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.Samples(id); len(got) != 0 {
		t.Errorf("samples not deleted: %d", len(got))
	}
}

func TestFinishOpenRides(t *testing.T) {
	s := open(t)
	start := time.Now().Truncate(time.Second)
	empty, _ := s.StartRide(start)
	crashed, _ := s.StartRide(start)
	s.AddSample(crashed, Sample{T: start, Power: 100, Mode: "erg"})
	s.AddSample(crashed, Sample{T: start.Add(time.Second), Power: 100, Mode: "erg"})

	if err := s.FinishOpenRides(); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Ride(empty); err != ErrNotFound {
		t.Errorf("empty open ride should be deleted: %v", err)
	}
	r, err := s.Ride(crashed)
	if err != nil || r.EndedAt == nil || !r.EndedAt.Equal(start.Add(time.Second)) || r.MovingS != 2 {
		t.Errorf("crashed ride = %+v, %v", r, err)
	}
}
