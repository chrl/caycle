// Package store keeps routes, rides and per-second ride samples in SQLite.
package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"caycle/internal/route"

	_ "modernc.org/sqlite"
)

// ErrNotFound is returned for unknown route or ride IDs.
var ErrNotFound = errors.New("not found")

const schema = `
CREATE TABLE IF NOT EXISTS routes (
	id          INTEGER PRIMARY KEY,
	name        TEXT    NOT NULL,
	created_at  INTEGER NOT NULL,
	distance_m  REAL    NOT NULL,
	ascent_m    REAL    NOT NULL,
	has_ele     INTEGER NOT NULL,
	points      TEXT    NOT NULL -- JSON [[lat, lon, ele], ...]
);
CREATE TABLE IF NOT EXISTS rides (
	id             INTEGER PRIMARY KEY,
	started_at     INTEGER NOT NULL,
	ended_at       INTEGER,
	name           TEXT    NOT NULL DEFAULT '',
	route_id       INTEGER REFERENCES routes(id) ON DELETE SET NULL,
	moving_s       REAL    NOT NULL DEFAULT 0,
	distance_m     REAL    NOT NULL DEFAULT 0,
	ascent_m       REAL    NOT NULL DEFAULT 0,
	avg_power      REAL    NOT NULL DEFAULT 0,
	max_power      INTEGER NOT NULL DEFAULT 0,
	avg_heart_rate REAL    NOT NULL DEFAULT 0,
	max_heart_rate INTEGER NOT NULL DEFAULT 0,
	avg_cadence    REAL    NOT NULL DEFAULT 0,
	energy_kj      REAL    NOT NULL DEFAULT 0,
	strava_url     TEXT
);
CREATE TABLE IF NOT EXISTS samples (
	ride_id     INTEGER NOT NULL REFERENCES rides(id) ON DELETE CASCADE,
	t           INTEGER NOT NULL, -- unix ms
	power       INTEGER NOT NULL,
	cadence     INTEGER NOT NULL,
	heart_rate  INTEGER NOT NULL,
	speed_kmh   REAL    NOT NULL,
	distance_m  REAL    NOT NULL,
	lat         REAL,
	lon         REAL,
	ele         REAL,
	grade       REAL,
	mode        TEXT    NOT NULL,
	target      REAL    NOT NULL
);
CREATE INDEX IF NOT EXISTS samples_ride ON samples(ride_id, t);
`

// Store is the caycle database.
type Store struct {
	db *sql.DB
}

// Open opens (and creates or migrates) the database at path.
func Open(path string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	dsn := "file:" + path + "?_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	// One connection keeps pragmas consistent and serialises writes.
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate %s: %w", path, err)
	}
	return &Store{db: db}, nil
}

func (s *Store) Close() error { return s.db.Close() }

func ms(t time.Time) int64     { return t.UnixMilli() }
func fromMS(v int64) time.Time { return time.UnixMilli(v) }
func notFound(err error) error {
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	return err
}

// --- routes ---

// RouteInfo describes a stored route.
type RouteInfo struct {
	ID        int64     `json:"id"`
	Name      string    `json:"name"`
	DistanceM float64   `json:"distanceM"`
	AscentM   float64   `json:"ascentM"`
	CreatedAt time.Time `json:"createdAt"`
}

// AddRoute stores a parsed route.
func (s *Store) AddRoute(r *route.Route) (RouteInfo, error) {
	pts := make([][3]float64, len(r.Points))
	for i, p := range r.Points {
		pts[i] = [3]float64{p.Lat, p.Lon, p.Ele}
	}
	data, err := json.Marshal(pts)
	if err != nil {
		return RouteInfo{}, err
	}
	info := RouteInfo{Name: r.Name, DistanceM: r.Distance(), AscentM: r.Ascent(), CreatedAt: time.Now().Truncate(time.Millisecond)}
	res, err := s.db.Exec(`INSERT INTO routes (name, created_at, distance_m, ascent_m, has_ele, points) VALUES (?, ?, ?, ?, ?, ?)`,
		info.Name, ms(info.CreatedAt), info.DistanceM, info.AscentM, r.HasEle, string(data))
	if err != nil {
		return RouteInfo{}, err
	}
	info.ID, err = res.LastInsertId()
	return info, err
}

// Routes lists stored routes, newest first.
func (s *Store) Routes() ([]RouteInfo, error) {
	rows, err := s.db.Query(`SELECT id, name, distance_m, ascent_m, created_at FROM routes ORDER BY id DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []RouteInfo{}
	for rows.Next() {
		var ri RouteInfo
		var created int64
		if err := rows.Scan(&ri.ID, &ri.Name, &ri.DistanceM, &ri.AscentM, &created); err != nil {
			return nil, err
		}
		ri.CreatedAt = fromMS(created)
		out = append(out, ri)
	}
	return out, rows.Err()
}

// Route loads a stored route.
func (s *Store) Route(id int64) (RouteInfo, *route.Route, error) {
	var (
		ri      RouteInfo
		created int64
		hasEle  bool
		data    string
	)
	err := s.db.QueryRow(`SELECT id, name, distance_m, ascent_m, created_at, has_ele, points FROM routes WHERE id = ?`, id).
		Scan(&ri.ID, &ri.Name, &ri.DistanceM, &ri.AscentM, &created, &hasEle, &data)
	if err != nil {
		return ri, nil, notFound(err)
	}
	ri.CreatedAt = fromMS(created)
	var pts [][3]float64
	if err := json.Unmarshal([]byte(data), &pts); err != nil {
		return ri, nil, fmt.Errorf("route %d: %w", id, err)
	}
	rp := make([]route.Point, len(pts))
	for i, p := range pts {
		rp[i] = route.Point{Lat: p[0], Lon: p[1], Ele: p[2]}
	}
	r, err := route.New(ri.Name, rp, hasEle)
	return ri, r, err
}

// DeleteRoute removes a route; rides that used it keep their samples.
func (s *Store) DeleteRoute(id int64) error {
	res, err := s.db.Exec(`DELETE FROM routes WHERE id = ?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// --- rides ---

// Ride is a ride with its totals (computed when the ride is finished).
type Ride struct {
	ID           int64      `json:"id"`
	StartedAt    time.Time  `json:"startedAt"`
	EndedAt      *time.Time `json:"endedAt"`
	Name         string     `json:"name"`
	RouteName    *string    `json:"routeName"`
	MovingS      float64    `json:"movingS"`
	DistanceM    float64    `json:"distanceM"`
	AscentM      float64    `json:"ascentM"`
	AvgPower     float64    `json:"avgPower"`
	MaxPower     int        `json:"maxPower"`
	AvgHeartRate float64    `json:"avgHeartRate"`
	MaxHeartRate int        `json:"maxHeartRate"`
	AvgCadence   float64    `json:"avgCadence"`
	EnergyKJ     float64    `json:"energyKJ"`
	StravaURL    *string    `json:"stravaUrl"`
}

// Sample is one second of a ride. Position fields are nil without a route.
type Sample struct {
	T         time.Time `json:"-"`
	TMS       int64     `json:"t"`
	Power     int       `json:"power"`
	Cadence   int       `json:"cadence"`
	HeartRate int       `json:"heartRate"`
	SpeedKmh  float64   `json:"speedKmh"`
	DistanceM float64   `json:"distanceM"`
	Lat       *float64  `json:"lat"`
	Lon       *float64  `json:"lon"`
	Ele       *float64  `json:"eleM"`
	Grade     *float64  `json:"grade"`
	Mode      string    `json:"mode"`
	Target    float64   `json:"target"`
}

// StartRide creates an unfinished ride.
func (s *Store) StartRide(start time.Time) (int64, error) {
	res, err := s.db.Exec(`INSERT INTO rides (started_at) VALUES (?)`, ms(start))
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// SetRideRoute records which route a ride followed (nil for none).
func (s *Store) SetRideRoute(id int64, routeID *int64) error {
	_, err := s.db.Exec(`UPDATE rides SET route_id = ? WHERE id = ?`, routeID, id)
	return err
}

// AddSample appends one sample to a ride.
func (s *Store) AddSample(rideID int64, x Sample) error {
	_, err := s.db.Exec(`INSERT INTO samples (ride_id, t, power, cadence, heart_rate, speed_kmh, distance_m, lat, lon, ele, grade, mode, target)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		rideID, ms(x.T), x.Power, x.Cadence, x.HeartRate, x.SpeedKmh, x.DistanceM, x.Lat, x.Lon, x.Ele, x.Grade, x.Mode, x.Target)
	return err
}

// FinishRide sets the end time and computes totals from the samples, which
// are recorded once per second while moving.
func (s *Store) FinishRide(id int64, end time.Time) (Ride, error) {
	_, err := s.db.Exec(`
		UPDATE rides SET
			ended_at       = ?,
			moving_s       = (SELECT COUNT(*)                 FROM samples WHERE ride_id = rides.id),
			distance_m     = (SELECT COALESCE(MAX(distance_m), 0) FROM samples WHERE ride_id = rides.id),
			avg_power      = (SELECT COALESCE(AVG(MAX(power, 0)), 0) FROM samples WHERE ride_id = rides.id),
			max_power      = (SELECT COALESCE(MAX(power), 0)  FROM samples WHERE ride_id = rides.id),
			avg_heart_rate = (SELECT COALESCE(AVG(heart_rate), 0) FROM samples WHERE ride_id = rides.id AND heart_rate > 0),
			max_heart_rate = (SELECT COALESCE(MAX(heart_rate), 0) FROM samples WHERE ride_id = rides.id),
			avg_cadence    = (SELECT COALESCE(AVG(cadence), 0) FROM samples WHERE ride_id = rides.id AND cadence > 0),
			energy_kj      = (SELECT COALESCE(SUM(MAX(power, 0)), 0) / 1000.0 FROM samples WHERE ride_id = rides.id)
		WHERE id = ?`, ms(end), id)
	if err != nil {
		return Ride{}, err
	}
	samples, err := s.Samples(id)
	if err != nil {
		return Ride{}, err
	}
	ascent, prev := 0.0, (*float64)(nil)
	for _, x := range samples {
		if x.Ele != nil {
			if prev != nil {
				ascent += max(0, *x.Ele-*prev)
			}
			prev = x.Ele
		}
	}
	if _, err := s.db.Exec(`UPDATE rides SET ascent_m = ? WHERE id = ?`, ascent, id); err != nil {
		return Ride{}, err
	}
	return s.Ride(id)
}

// FinishOpenRides finalises rides left open by a crash. Rides without any
// samples are deleted.
func (s *Store) FinishOpenRides() error {
	if _, err := s.db.Exec(`DELETE FROM rides WHERE ended_at IS NULL AND NOT EXISTS (SELECT 1 FROM samples WHERE ride_id = rides.id)`); err != nil {
		return err
	}
	rows, err := s.db.Query(`SELECT rides.id, MAX(samples.t) FROM rides JOIN samples ON samples.ride_id = rides.id WHERE ended_at IS NULL GROUP BY rides.id`)
	if err != nil {
		return err
	}
	type open struct{ id, last int64 }
	var opens []open
	for rows.Next() {
		var o open
		if err := rows.Scan(&o.id, &o.last); err != nil {
			rows.Close()
			return err
		}
		opens = append(opens, o)
	}
	rows.Close()
	for _, o := range opens {
		if _, err := s.FinishRide(o.id, fromMS(o.last)); err != nil {
			return err
		}
	}
	return nil
}

const rideColumns = `rides.id, started_at, ended_at, rides.name, routes.name, moving_s, rides.distance_m, rides.ascent_m,
	avg_power, max_power, avg_heart_rate, max_heart_rate, avg_cadence, energy_kj, strava_url`

func scanRide(sc interface{ Scan(...any) error }) (Ride, error) {
	var (
		r         Ride
		start     int64
		end       sql.NullInt64
		routeName sql.NullString
		stravaURL sql.NullString
	)
	err := sc.Scan(&r.ID, &start, &end, &r.Name, &routeName, &r.MovingS, &r.DistanceM, &r.AscentM,
		&r.AvgPower, &r.MaxPower, &r.AvgHeartRate, &r.MaxHeartRate, &r.AvgCadence, &r.EnergyKJ, &stravaURL)
	if err != nil {
		return r, err
	}
	r.StartedAt = fromMS(start)
	if end.Valid {
		t := fromMS(end.Int64)
		r.EndedAt = &t
	}
	if routeName.Valid {
		r.RouteName = &routeName.String
	}
	if stravaURL.Valid {
		r.StravaURL = &stravaURL.String
	}
	return r, nil
}

// Rides lists rides, newest first.
func (s *Store) Rides() ([]Ride, error) {
	rows, err := s.db.Query(`SELECT ` + rideColumns + ` FROM rides LEFT JOIN routes ON routes.id = rides.route_id ORDER BY rides.started_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Ride{}
	for rows.Next() {
		r, err := scanRide(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// Ride loads one ride.
func (s *Store) Ride(id int64) (Ride, error) {
	r, err := scanRide(s.db.QueryRow(`SELECT `+rideColumns+` FROM rides LEFT JOIN routes ON routes.id = rides.route_id WHERE rides.id = ?`, id))
	return r, notFound(err)
}

// Samples returns a ride's samples in time order.
func (s *Store) Samples(rideID int64) ([]Sample, error) {
	rows, err := s.db.Query(`SELECT t, power, cadence, heart_rate, speed_kmh, distance_m, lat, lon, ele, grade, mode, target
		FROM samples WHERE ride_id = ? ORDER BY t`, rideID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Sample{}
	for rows.Next() {
		var x Sample
		if err := rows.Scan(&x.TMS, &x.Power, &x.Cadence, &x.HeartRate, &x.SpeedKmh, &x.DistanceM,
			&x.Lat, &x.Lon, &x.Ele, &x.Grade, &x.Mode, &x.Target); err != nil {
			return nil, err
		}
		x.T = fromMS(x.TMS)
		out = append(out, x)
	}
	return out, rows.Err()
}

// SetName renames a ride.
func (s *Store) SetName(id int64, name string) error {
	_, err := s.db.Exec(`UPDATE rides SET name = ? WHERE id = ?`, name, id)
	return err
}

// SetStravaURL records where a ride was uploaded.
func (s *Store) SetStravaURL(id int64, url string) error {
	_, err := s.db.Exec(`UPDATE rides SET strava_url = ? WHERE id = ?`, url, id)
	return err
}

// ClearStravaURL forgets a ride's upload, so it can be uploaded again.
func (s *Store) ClearStravaURL(id int64) error {
	_, err := s.db.Exec(`UPDATE rides SET strava_url = NULL WHERE id = ?`, id)
	return err
}

// DeleteRide removes a ride and its samples.
func (s *Store) DeleteRide(id int64) error {
	res, err := s.db.Exec(`DELETE FROM rides WHERE id = ?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}
