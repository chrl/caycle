package tcx

import (
	"bytes"
	"encoding/xml"
	"strings"
	"testing"
	"time"
)

func TestWrite(t *testing.T) {
	start := time.Date(2026, 10, 8, 19, 30, 0, 0, time.UTC)
	a := Activity{
		Start:     start,
		Duration:  2 * time.Second,
		DistanceM: 18.5,
		Calories:  1,
		Samples: []Sample{
			{Time: start, Power: 210, Cadence: 90, HeartRate: 140, SpeedMps: 9.2, DistanceM: 9.2},
			{Time: start.Add(time.Second), Power: -1, Cadence: -1, SpeedMps: 9.3, DistanceM: 18.5,
				Lat: new(52.5), Lon: new(13.25), Ele: new(34.5)},
		},
	}
	var buf bytes.Buffer
	if err := Write(&buf, a); err != nil {
		t.Fatal(err)
	}
	out := buf.String()

	if !strings.HasPrefix(out, "<?xml") {
		t.Errorf("output must start with the XML declaration, got %q", out[:20])
	}
	for _, want := range []string{
		`<TrainingCenterDatabase xmlns="http://www.garmin.com/xmlschemas/TrainingCenterDatabase/v2" xmlns:ns3="http://www.garmin.com/xmlschemas/ActivityExtension/v2">`,
		`<Activity Sport="Biking">`,
		`<Id>2026-10-08T19:30:00Z</Id>`,
		`<Lap StartTime="2026-10-08T19:30:00Z">`,
		`<TotalTimeSeconds>2</TotalTimeSeconds>`,
		`<HeartRateBpm>`, `<Value>140</Value>`,
		`<Cadence>90</Cadence>`,
		`<ns3:TPX>`, `<ns3:Speed>9.2</ns3:Speed>`, `<ns3:Watts>210</ns3:Watts>`,
		`<Position>`, `<LatitudeDegrees>52.5</LatitudeDegrees>`, `<LongitudeDegrees>13.25</LongitudeDegrees>`,
		`<AltitudeMeters>34.5</AltitudeMeters>`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %s in:\n%s", want, out)
		}
	}
	// Unknown values must be omitted rather than written as -1.
	if strings.Count(out, "<Position>") != 1 || strings.Count(out, "<ns3:Watts>") != 1 || strings.Count(out, "<Cadence>") != 1 || strings.Contains(out, ">-1<") {
		t.Errorf("unknown values written:\n%s", out)
	}
	if err := xml.Unmarshal(buf.Bytes(), new(struct{})); err != nil {
		t.Errorf("not well-formed XML: %v", err)
	}
}
