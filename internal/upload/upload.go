// Package upload turns stored rides into TCX files and sends them to Strava.
package upload

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"time"

	"caycle/internal/store"
	"caycle/internal/strava"
	"caycle/internal/tcx"
)

// Uploader exports rides from the store.
type Uploader struct {
	Store     *store.Store
	CredsPath string // Strava credentials
	RidesDir  string // where TCX files are written
}

// StravaConnected reports whether Strava credentials are stored.
func (u *Uploader) StravaConnected() bool {
	_, err := strava.Load(u.CredsPath)
	return err == nil
}

// WriteTCX exports a finished ride and returns the file path. It also reports
// whether the ride has a virtual position (was ridden on a route).
func (u *Uploader) WriteTCX(id int64) (string, bool, error) {
	ride, err := u.Store.Ride(id)
	if err != nil {
		return "", false, err
	}
	if ride.EndedAt == nil {
		return "", false, errors.New("ride is still being recorded")
	}
	samples, err := u.Store.Samples(id)
	if err != nil {
		return "", false, err
	}
	if len(samples) == 0 {
		return "", false, errors.New("ride has no data")
	}
	a := tcx.Activity{
		Start:     ride.StartedAt,
		Duration:  time.Duration(ride.MovingS) * time.Second,
		DistanceM: ride.DistanceM,
		Calories:  int(ride.EnergyKJ), // ~1 kcal burned per kJ of work on a bike
	}
	virtual := false
	for _, x := range samples {
		a.Samples = append(a.Samples, tcx.Sample{
			Time: x.T, Power: x.Power, Cadence: x.Cadence, HeartRate: x.HeartRate,
			SpeedMps: x.SpeedKmh / 3.6, DistanceM: x.DistanceM, Lat: x.Lat, Lon: x.Lon, Ele: x.Ele,
		})
		virtual = virtual || x.Lat != nil
	}

	if err := os.MkdirAll(u.RidesDir, 0o755); err != nil {
		return "", false, err
	}
	path := filepath.Join(u.RidesDir, ride.StartedAt.Format("2006-01-02-150405")+".tcx")
	f, err := os.Create(path)
	if err != nil {
		return "", false, err
	}
	if err := tcx.Write(f, a); err != nil {
		f.Close()
		return "", false, err
	}
	return path, virtual, f.Close()
}

// Strava uploads a finished ride and returns the activity URL. Rides on a
// route upload as virtual rides, so they stay off real-world segment
// leaderboards. An already uploaded ride returns its existing URL.
func (u *Uploader) Strava(ctx context.Context, id int64, name string) (string, error) {
	ride, err := u.Store.Ride(id)
	if err != nil {
		return "", err
	}
	if ride.StravaURL != nil {
		return *ride.StravaURL, nil
	}
	client, err := strava.Load(u.CredsPath)
	if err != nil {
		return "", err
	}
	path, virtual, err := u.WriteTCX(id)
	if err != nil {
		return "", err
	}
	if name == "" {
		name = ride.Name
	}
	if name == "" && ride.RouteName != nil {
		name = *ride.RouteName
	}
	// Free rides are trainer rides; route rides are virtual rides with a map
	// (Strava hides the map when the trainer flag is set).
	opts := strava.UploadOptions{Name: name, ExternalID: filepath.Base(path), Trainer: !virtual}
	if virtual {
		opts.SportType = "VirtualRide"
	}
	url, err := client.Upload(ctx, path, opts)
	if err != nil {
		return "", err
	}
	if name != "" && name != ride.Name {
		u.Store.SetName(id, name)
	}
	return url, u.Store.SetStravaURL(id, url)
}
