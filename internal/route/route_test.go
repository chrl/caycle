package route

import (
	"fmt"
	"math"
	"strings"
	"testing"
)

// straightClimb builds a GPX track heading north: flat for 1 km, then 1 km at
// a constant 5 % grade, with points every ~11 m.
func straightClimb(withEle bool) string {
	var b strings.Builder
	b.WriteString(`<?xml version="1.0"?><gpx xmlns="http://www.topografix.com/GPX/1/1"><metadata><name>Test climb</name></metadata><trk><trkseg>`)
	const degPerM = 1 / 111195.0
	for i := 0; i <= 180; i++ {
		d := float64(i) * 11.1195
		ele := 100.0
		if d > 1000 {
			ele += (d - 1000) * 0.05
		}
		if withEle {
			fmt.Fprintf(&b, `<trkpt lat="%.7f" lon="13.4"><ele>%.2f</ele></trkpt>`, 52+d*degPerM, ele)
		} else {
			fmt.Fprintf(&b, `<trkpt lat="%.7f" lon="13.4"/>`, 52+d*degPerM)
		}
	}
	b.WriteString(`</trkseg></trk></gpx>`)
	return b.String()
}

func near(a, b, tol float64) bool { return math.Abs(a-b) <= tol }

func TestParseAndGrade(t *testing.T) {
	r, err := ParseGPX(strings.NewReader(straightClimb(true)))
	if err != nil {
		t.Fatal(err)
	}
	if r.Name != "Test climb" || !r.HasEle {
		t.Errorf("name %q hasEle %v", r.Name, r.HasEle)
	}
	if !near(r.Distance(), 2001.5, 2) {
		t.Errorf("distance = %.1f", r.Distance())
	}
	if g := r.Grade(500); !near(g, 0, 0.01) {
		t.Errorf("flat grade = %.2f", g)
	}
	if g := r.Grade(1500); !near(g, 5, 0.05) {
		t.Errorf("climb grade = %.2f", g)
	}
	// Smoothing softens the transition instead of jumping from 0 to 5 %.
	if g := r.Grade(1000); g <= 0.5 || g >= 4.5 {
		t.Errorf("transition grade = %.2f, want something in between", g)
	}
	if a := r.Ascent(); !near(a, 50, 2) {
		t.Errorf("ascent = %.1f", a)
	}
	lat, lon, ele := r.At(1500)
	if !near(lat, 52+1500/111195.0, 1e-6) || lon != 13.4 || !near(ele, 125, 0.5) {
		t.Errorf("At(1500) = %f %f %f", lat, lon, ele)
	}
	// Past the end clamps to the last point.
	if lat, _, _ := r.At(5000); !near(lat, r.Points[len(r.Points)-1].Lat, 1e-9) {
		t.Errorf("At past end = %f", lat)
	}
	ds := r.Downsample(50)
	if len(ds) != 50 || ds[0].Dist != 0 || !near(ds[49].Dist, r.Distance(), 1e-6) {
		t.Errorf("downsample: %d points, first %v last %v", len(ds), ds[0], ds[len(ds)-1])
	}
}

func TestNoElevation(t *testing.T) {
	r, err := ParseGPX(strings.NewReader(straightClimb(false)))
	if err != nil {
		t.Fatal(err)
	}
	if r.HasEle || r.Grade(1500) != 0 || r.Ascent() != 0 {
		t.Errorf("route without elevation should be flat: %v %v %v", r.HasEle, r.Grade(1500), r.Ascent())
	}
}

func TestRoutePoints(t *testing.T) {
	gpx := `<gpx><rte><name>R</name><rtept lat="52" lon="13"><ele>1</ele></rtept><rtept lat="52.01" lon="13"><ele>2</ele></rtept></rte></gpx>`
	r, err := ParseGPX(strings.NewReader(gpx))
	if err != nil {
		t.Fatal(err)
	}
	if r.Name != "R" || len(r.Points) != 2 {
		t.Errorf("got %q with %d points", r.Name, len(r.Points))
	}
}

func TestInvalid(t *testing.T) {
	if _, err := ParseGPX(strings.NewReader("hello")); err == nil {
		t.Error("expected error for garbage")
	}
	if _, err := ParseGPX(strings.NewReader(`<gpx><trk><trkseg><trkpt lat="1" lon="1"/></trkseg></trk></gpx>`)); err == nil {
		t.Error("expected error for single point")
	}
}
