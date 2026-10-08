package server

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"caycle/internal/session"
	"caycle/internal/store"
	"caycle/internal/trainer"
	"caycle/internal/upload"
)

const gpx = `<?xml version="1.0"?><gpx><trk><name>Loop</name><trkseg>
<trkpt lat="52.000" lon="13.0"><ele>10</ele></trkpt>
<trkpt lat="52.005" lon="13.0"><ele>30</ele></trkpt>
<trkpt lat="52.010" lon="13.0"><ele>20</ele></trkpt>
</trkseg></trk></gpx>`

func setup(t *testing.T) (*httptest.Server, *session.Session) {
	t.Helper()
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	demo := trainer.NewDemo()
	sess := session.New(session.Config{Store: st, Trainer: func() (session.Device, string) { return demo, "connected" }})
	stop := make(chan struct{})
	t.Cleanup(func() { close(stop) })
	go sess.Run(stop)
	srv := httptest.NewServer((&Server{
		Session:  sess,
		Store:    st,
		Uploader: &upload.Uploader{Store: st, CredsPath: filepath.Join(dir, "none.json"), RidesDir: dir},
	}).Handler())
	t.Cleanup(srv.Close)
	return srv, sess
}

func call(t *testing.T, method, url, contentType string, body []byte, wantStatus int, out any) {
	t.Helper()
	req, _ := http.NewRequest(method, url, bytes.NewReader(body))
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != wantStatus {
		var e map[string]string
		json.NewDecoder(resp.Body).Decode(&e)
		t.Fatalf("%s %s: status %d (%v), want %d", method, url, resp.StatusCode, e, wantStatus)
	}
	if out != nil {
		if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
			t.Fatalf("%s %s: decode: %v", method, url, err)
		}
	}
}

func TestRideFlow(t *testing.T) {
	srv, sess := setup(t)
	api := srv.URL + "/api"

	// Upload a route.
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	fw, _ := mw.CreateFormFile("file", "loop.gpx")
	fw.Write([]byte(gpx))
	mw.Close()
	var info store.RouteInfo
	call(t, "POST", api+"/routes", mw.FormDataContentType(), body.Bytes(), http.StatusCreated, &info)
	if info.Name != "Loop" || info.DistanceM < 1000 {
		t.Fatalf("route %+v", info)
	}
	var detail struct {
		Points [][4]float64 `json:"points"`
	}
	call(t, "GET", fmt.Sprintf("%s/routes/%d", api, info.ID), "", nil, http.StatusOK, &detail)
	if len(detail.Points) != 3 || detail.Points[2][3] < 1000 {
		t.Fatalf("points %v", detail.Points)
	}
	call(t, "POST", api+"/routes", "text/plain", []byte("x"), http.StatusBadRequest, nil)

	// Route mode needs a route; select one, then ride.
	call(t, "POST", api+"/control", "application/json", []byte(`{"mode":"bogus"}`), http.StatusBadRequest, nil)
	call(t, "POST", api+"/route/select", "application/json", []byte(fmt.Sprintf(`{"id":%d}`, info.ID)), http.StatusNoContent, nil)
	call(t, "POST", api+"/control", "application/json", []byte(`{"step":-1}`), http.StatusNoContent, nil)
	if c := sess.Snapshot().Control; c.Mode != session.RouteMode || c.Difficulty != 90 {
		t.Fatalf("control %+v", c)
	}
	var started struct{ ID int64 }
	call(t, "POST", api+"/ride/start", "", nil, http.StatusOK, &started)
	call(t, "POST", api+"/ride/start", "", nil, http.StatusConflict, nil)

	// Live stream delivers snapshots.
	resp, err := http.Get(api + "/live")
	if err != nil {
		t.Fatal(err)
	}
	sc := bufio.NewScanner(resp.Body)
	var snap session.Snapshot
	for sc.Scan() {
		if line, ok := strings.CutPrefix(sc.Text(), "data: "); ok {
			if err := json.Unmarshal([]byte(line), &snap); err != nil {
				t.Fatal(err)
			}
			break
		}
	}
	resp.Body.Close()
	if !snap.Ride.Recording || snap.Route == nil || snap.Trainer.Name != "Demo trainer" {
		t.Fatalf("snapshot %+v", snap)
	}

	time.Sleep(1300 * time.Millisecond) // record at least one sample
	call(t, "DELETE", fmt.Sprintf("%s/rides/%d", api, started.ID), "", nil, http.StatusConflict, nil)
	var ride store.Ride
	call(t, "POST", api+"/ride/finish", "", nil, http.StatusOK, &ride)
	if ride.ID != started.ID || ride.MovingS < 1 || ride.RouteName == nil {
		t.Fatalf("ride %+v", ride)
	}
	call(t, "POST", api+"/ride/finish", "", nil, http.StatusConflict, nil)

	var rides []store.Ride
	call(t, "GET", api+"/rides", "", nil, http.StatusOK, &rides)
	if len(rides) != 1 {
		t.Fatalf("rides %+v", rides)
	}
	var samples []map[string]any
	call(t, "GET", fmt.Sprintf("%s/rides/%d/samples", api, ride.ID), "", nil, http.StatusOK, &samples)
	if len(samples) == 0 || samples[0]["t"] == nil || samples[0]["eleM"] == nil {
		t.Fatalf("samples %v", samples)
	}
	// Not logged in to Strava.
	call(t, "POST", fmt.Sprintf("%s/rides/%d/strava", api, ride.ID), "", nil, http.StatusBadRequest, nil)

	call(t, "DELETE", fmt.Sprintf("%s/rides/%d", api, ride.ID), "", nil, http.StatusNoContent, nil)
	call(t, "DELETE", fmt.Sprintf("%s/routes/%d", api, info.ID), "", nil, http.StatusNoContent, nil)
	if sess.Snapshot().Route != nil {
		t.Error("deleting the selected route should clear it")
	}
	call(t, "GET", api+"/nope", "", nil, http.StatusNotFound, nil)

	// The dashboard is served for unknown paths too.
	resp, err = http.Get(srv.URL + "/history")
	if err != nil || resp.StatusCode != http.StatusOK || !strings.HasPrefix(resp.Header.Get("Content-Type"), "text/html") {
		t.Fatalf("static: %v %v", resp, err)
	}
	resp.Body.Close()
}
