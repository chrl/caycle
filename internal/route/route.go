// Package route loads GPX routes and answers "where am I and how steep is it"
// for a distance ridden along the route.
package route

import (
	"encoding/xml"
	"errors"
	"io"
	"math"
	"sort"
)

// Point is a route point; Dist is the distance from the start in meters.
type Point struct {
	Lat, Lon, Ele, Dist float64
}

// Route is a parsed route with a smoothed elevation profile.
type Route struct {
	Name   string
	Points []Point
	HasEle bool

	profile []float64 // smoothed elevation every profileStep meters
}

const (
	profileStep = 10.0 // m between elevation profile samples
	smoothHalf  = 3    // moving average over ±3 samples (±30 m)
	gradeHalf   = 20.0 // grade is measured over ±20 m
	maxGrade    = 25.0 // %
)

type gpxPoint struct {
	Lat float64  `xml:"lat,attr"`
	Lon float64  `xml:"lon,attr"`
	Ele *float64 `xml:"ele"`
}

type gpxFile struct {
	Name     string     `xml:"metadata>name"`
	TrkName  string     `xml:"trk>name"`
	RteName  string     `xml:"rte>name"`
	Track    []gpxPoint `xml:"trk>trkseg>trkpt"`
	RoutePts []gpxPoint `xml:"rte>rtept"`
}

// ParseGPX reads track points (or route points if there is no track).
func ParseGPX(r io.Reader) (*Route, error) {
	var g gpxFile
	if err := xml.NewDecoder(r).Decode(&g); err != nil {
		return nil, errors.New("not a valid GPX file")
	}
	src := g.Track
	if len(src) == 0 {
		src = g.RoutePts
	}
	pts := make([]Point, 0, len(src))
	hasEle := false
	for _, p := range src {
		pt := Point{Lat: p.Lat, Lon: p.Lon}
		if p.Ele != nil {
			pt.Ele, hasEle = *p.Ele, true
		}
		pts = append(pts, pt)
	}
	name := g.Name
	for _, n := range []string{g.TrkName, g.RteName} {
		if name == "" {
			name = n
		}
	}
	return New(name, pts, hasEle)
}

// New builds a route from points; Dist is computed from coordinates.
func New(name string, pts []Point, hasEle bool) (*Route, error) {
	if len(pts) < 2 {
		return nil, errors.New("route needs at least two points")
	}
	pts = append([]Point(nil), pts...)
	pts[0].Dist = 0
	for i := 1; i < len(pts); i++ {
		pts[i].Dist = pts[i-1].Dist + haversine(pts[i-1], pts[i])
	}
	if pts[len(pts)-1].Dist < 100 {
		return nil, errors.New("route is shorter than 100 m")
	}
	r := &Route{Name: name, Points: pts, HasEle: hasEle}
	r.buildProfile()
	return r, nil
}

func haversine(a, b Point) float64 {
	const earthR = 6371000.0
	rad := math.Pi / 180
	dLat := (b.Lat - a.Lat) * rad
	dLon := (b.Lon - a.Lon) * rad
	h := math.Sin(dLat/2)*math.Sin(dLat/2) +
		math.Cos(a.Lat*rad)*math.Cos(b.Lat*rad)*math.Sin(dLon/2)*math.Sin(dLon/2)
	return 2 * earthR * math.Asin(math.Sqrt(h))
}

// Distance is the total route length in meters.
func (r *Route) Distance() float64 { return r.Points[len(r.Points)-1].Dist }

// segment returns i such that Points[i].Dist <= d <= Points[i+1].Dist, and the
// interpolation fraction within that segment.
func (r *Route) segment(d float64) (int, float64) {
	d = max(0, min(d, r.Distance()))
	i := sort.Search(len(r.Points), func(i int) bool { return r.Points[i].Dist > d }) - 1
	i = max(0, min(i, len(r.Points)-2))
	a, b := r.Points[i], r.Points[i+1]
	if b.Dist == a.Dist {
		return i, 0
	}
	return i, (d - a.Dist) / (b.Dist - a.Dist)
}

func (r *Route) rawEle(d float64) float64 {
	i, f := r.segment(d)
	return r.Points[i].Ele + f*(r.Points[i+1].Ele-r.Points[i].Ele)
}

// buildProfile resamples elevation every profileStep meters and smooths it,
// because GPX elevation is noisy enough to produce silly grades otherwise.
func (r *Route) buildProfile() {
	n := int(r.Distance()/profileStep) + 2
	raw := make([]float64, n)
	for i := range raw {
		raw[i] = r.rawEle(float64(i) * profileStep)
	}
	r.profile = make([]float64, n)
	for i := range raw {
		lo, hi := max(0, i-smoothHalf), min(n-1, i+smoothHalf)
		sum := 0.0
		for j := lo; j <= hi; j++ {
			sum += raw[j]
		}
		r.profile[i] = sum / float64(hi-lo+1)
	}
}

// Elevation is the smoothed elevation at distance d.
func (r *Route) Elevation(d float64) float64 {
	d = max(0, min(d, r.Distance()))
	x := d / profileStep
	i := min(int(x), len(r.profile)-2)
	f := x - float64(i)
	return r.profile[i] + f*(r.profile[i+1]-r.profile[i])
}

// Grade is the road grade in percent at distance d.
func (r *Route) Grade(d float64) float64 {
	if !r.HasEle {
		return 0
	}
	lo := max(0, d-gradeHalf)
	hi := min(r.Distance(), d+gradeHalf)
	if hi-lo < 1 {
		return 0
	}
	g := (r.Elevation(hi) - r.Elevation(lo)) / (hi - lo) * 100
	return max(-maxGrade, min(maxGrade, g))
}

// Ascent is the total climbing in meters, from the smoothed profile.
func (r *Route) Ascent() float64 {
	if !r.HasEle {
		return 0
	}
	sum := 0.0
	for i := 1; i < len(r.profile); i++ {
		sum += max(0, r.profile[i]-r.profile[i-1])
	}
	return sum
}

// At returns the position and smoothed elevation at distance d.
func (r *Route) At(d float64) (lat, lon, ele float64) {
	i, f := r.segment(d)
	a, b := r.Points[i], r.Points[i+1]
	return a.Lat + f*(b.Lat-a.Lat), a.Lon + f*(b.Lon-a.Lon), r.Elevation(d)
}

// Downsample returns at most n points evenly spread along the route, with
// smoothed elevation, for drawing maps and profiles.
func (r *Route) Downsample(n int) []Point {
	n = max(2, n)
	if len(r.Points) <= n {
		out := make([]Point, len(r.Points))
		for i, p := range r.Points {
			p.Ele = r.Elevation(p.Dist)
			out[i] = p
		}
		return out
	}
	out := make([]Point, n)
	for i := range out {
		d := r.Distance() * float64(i) / float64(n-1)
		lat, lon, ele := r.At(d)
		out[i] = Point{Lat: lat, Lon: lon, Ele: ele, Dist: d}
	}
	return out
}
