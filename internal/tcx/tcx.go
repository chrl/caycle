// Package tcx writes rides as Garmin Training Center XML (TCX v2) files,
// which Strava and most training platforms accept.
package tcx

import (
	"encoding/xml"
	"io"
	"time"
)

// Sample is one recorded data point, typically one per second.
type Sample struct {
	Time      time.Time
	Power     int // W, <0 if unknown
	Cadence   int // rpm, <0 if unknown
	HeartRate int // bpm, 0 if unknown
	SpeedMps  float64
	DistanceM float64 // accumulated
	// Optional virtual position (route rides).
	Lat, Lon, Ele *float64
}

// Activity is a single-lap indoor bike ride.
type Activity struct {
	Start     time.Time
	Duration  time.Duration // moving time
	DistanceM float64
	Calories  int
	Samples   []Sample
}

type document struct {
	XMLName  xml.Name `xml:"TrainingCenterDatabase"`
	Xmlns    string   `xml:"xmlns,attr"`
	XmlnsNS3 string   `xml:"xmlns:ns3,attr"`
	Activity activity `xml:"Activities>Activity"`
}

type activity struct {
	Sport string `xml:"Sport,attr"`
	ID    string `xml:"Id"`
	Lap   lap    `xml:"Lap"`
}

type lap struct {
	StartTime        string       `xml:"StartTime,attr"`
	TotalTimeSeconds float64      `xml:"TotalTimeSeconds"`
	DistanceMeters   float64      `xml:"DistanceMeters"`
	Calories         int          `xml:"Calories"`
	Intensity        string       `xml:"Intensity"`
	TriggerMethod    string       `xml:"TriggerMethod"`
	Trackpoints      []trackpoint `xml:"Track>Trackpoint"`
}

type trackpoint struct {
	Time           string    `xml:"Time"`
	Position       *position `xml:"Position,omitempty"`
	AltitudeMeters *float64  `xml:"AltitudeMeters,omitempty"`
	DistanceMeters float64   `xml:"DistanceMeters"`
	HeartRate      *int      `xml:"HeartRateBpm>Value,omitempty"`
	Cadence        *int      `xml:"Cadence,omitempty"`
	Extensions     tpx       `xml:"Extensions>ns3:TPX"`
}

type position struct {
	Lat float64 `xml:"LatitudeDegrees"`
	Lon float64 `xml:"LongitudeDegrees"`
}

type tpx struct {
	Speed float64 `xml:"ns3:Speed"`
	Watts *int    `xml:"ns3:Watts,omitempty"`
}

func ts(t time.Time) string { return t.UTC().Format(time.RFC3339) }

// Write encodes the activity as TCX.
func Write(w io.Writer, a Activity) error {
	doc := document{
		Xmlns:    "http://www.garmin.com/xmlschemas/TrainingCenterDatabase/v2",
		XmlnsNS3: "http://www.garmin.com/xmlschemas/ActivityExtension/v2",
		Activity: activity{
			Sport: "Biking",
			ID:    ts(a.Start),
			Lap: lap{
				StartTime:        ts(a.Start),
				TotalTimeSeconds: a.Duration.Round(time.Second).Seconds(),
				DistanceMeters:   a.DistanceM,
				Calories:         a.Calories,
				Intensity:        "Active",
				TriggerMethod:    "Manual",
			},
		},
	}
	for _, s := range a.Samples {
		tp := trackpoint{
			Time:           ts(s.Time),
			DistanceMeters: s.DistanceM,
			Extensions:     tpx{Speed: s.SpeedMps},
			AltitudeMeters: s.Ele,
		}
		if s.Lat != nil && s.Lon != nil {
			tp.Position = &position{Lat: *s.Lat, Lon: *s.Lon}
		}
		if s.HeartRate > 0 {
			tp.HeartRate = new(s.HeartRate)
		}
		if s.Cadence >= 0 {
			tp.Cadence = new(min(s.Cadence, 254))
		}
		if s.Power >= 0 {
			tp.Extensions.Watts = new(s.Power)
		}
		doc.Activity.Lap.Trackpoints = append(doc.Activity.Lap.Trackpoints, tp)
	}

	// Strava rejects TCX files with anything before the XML declaration.
	if _, err := io.WriteString(w, xml.Header); err != nil {
		return err
	}
	enc := xml.NewEncoder(w)
	enc.Indent("", " ")
	if err := enc.Encode(doc); err != nil {
		return err
	}
	_, err := io.WriteString(w, "\n")
	return err
}
