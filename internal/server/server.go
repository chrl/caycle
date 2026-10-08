// Package server exposes the session, routes and ride history over HTTP for
// the web dashboard, with live data as Server-Sent Events.
package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"caycle/internal/route"
	"caycle/internal/session"
	"caycle/internal/store"
	"caycle/internal/strava"
	"caycle/internal/upload"
	"caycle/web"
)

// Server serves the API and the embedded dashboard.
type Server struct {
	Session  *session.Session
	Store    *store.Store
	Uploader *upload.Uploader
}

// Handler returns the HTTP handler.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/live", s.live)
	mux.HandleFunc("POST /api/control", s.control)
	mux.HandleFunc("POST /api/ride/start", s.startRide)
	mux.HandleFunc("POST /api/ride/finish", s.finishRide)
	mux.HandleFunc("GET /api/routes", s.listRoutes)
	mux.HandleFunc("POST /api/routes", s.addRoute)
	mux.HandleFunc("GET /api/routes/{id}", s.getRoute)
	mux.HandleFunc("DELETE /api/routes/{id}", s.deleteRoute)
	mux.HandleFunc("POST /api/route/select", s.selectRoute)
	mux.HandleFunc("GET /api/rides", s.listRides)
	mux.HandleFunc("GET /api/rides/{id}/samples", s.rideSamples)
	mux.HandleFunc("POST /api/rides/{id}/strava", s.uploadRide)
	mux.HandleFunc("DELETE /api/rides/{id}", s.deleteRide)
	mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) {
		writeError(w, http.StatusNotFound, errors.New("no such endpoint"))
	})
	mux.Handle("/", staticHandler())
	return mux
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, err error) {
	writeJSON(w, status, map[string]string{"error": err.Error()})
}

func storeError(w http.ResponseWriter, err error) {
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, err)
		return
	}
	writeError(w, http.StatusInternalServerError, err)
}

func pathID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, errors.New("invalid id"))
		return 0, false
	}
	return id, true
}

func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(v); err != nil {
		writeError(w, http.StatusBadRequest, fmt.Errorf("invalid JSON: %w", err))
		return false
	}
	return true
}

// live streams snapshots ~4 times per second.
func (s *Server) live(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, errors.New("streaming unsupported"))
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no")

	tick := time.NewTicker(250 * time.Millisecond)
	defer tick.Stop()
	stravaConnected, stravaChecked := false, time.Time{}
	for {
		if time.Since(stravaChecked) > 5*time.Second {
			stravaConnected, stravaChecked = s.Uploader.StravaConnected(), time.Now()
		}
		snap := s.Session.Snapshot()
		snap.Strava.Connected = stravaConnected
		data, err := json.Marshal(snap)
		if err != nil {
			return
		}
		if _, err := fmt.Fprintf(w, "data: %s\n\n", data); err != nil {
			return
		}
		flusher.Flush()
		select {
		case <-r.Context().Done():
			return
		case <-tick.C:
		}
	}
}

func (s *Server) control(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Mode  *session.Mode `json:"mode"`
		Step  *int          `json:"step"`
		Value *float64      `json:"value"`
	}
	if !decode(w, r, &req) {
		return
	}
	if req.Mode != nil {
		if err := s.Session.SetMode(*req.Mode); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
	}
	if req.Value != nil {
		s.Session.SetValue(*req.Value)
	}
	if req.Step != nil {
		s.Session.Step(*req.Step)
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) startRide(w http.ResponseWriter, r *http.Request) {
	id, err := s.Session.StartRide()
	if err != nil {
		writeError(w, http.StatusConflict, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]int64{"id": id})
}

func (s *Server) finishRide(w http.ResponseWriter, r *http.Request) {
	ride, err := s.Session.FinishRide()
	if errors.Is(err, session.ErrNotRecording) {
		writeError(w, http.StatusConflict, err)
		return
	}
	if err != nil {
		storeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, ride)
}

func (s *Server) listRoutes(w http.ResponseWriter, r *http.Request) {
	routes, err := s.Store.Routes()
	if err != nil {
		storeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, routes)
}

func (s *Server) addRoute(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		writeError(w, http.StatusBadRequest, errors.New("expected a multipart form with a GPX file"))
		return
	}
	f, hdr, err := r.FormFile("file")
	if err != nil {
		writeError(w, http.StatusBadRequest, errors.New("missing file"))
		return
	}
	defer f.Close()
	rt, err := route.ParseGPX(f)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if name := strings.TrimSpace(r.FormValue("name")); name != "" {
		rt.Name = name
	}
	if rt.Name == "" {
		rt.Name = strings.TrimSuffix(hdr.Filename, filepath.Ext(hdr.Filename))
	}
	info, err := s.Store.AddRoute(rt)
	if err != nil {
		storeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, info)
}

func (s *Server) getRoute(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	info, rt, err := s.Store.Route(id)
	if err != nil {
		storeError(w, err)
		return
	}
	pts := rt.Downsample(2000)
	out := make([][4]float64, len(pts))
	for i, p := range pts {
		out[i] = [4]float64{p.Lat, p.Lon, p.Ele, p.Dist}
	}
	writeJSON(w, http.StatusOK, struct {
		store.RouteInfo
		Points [][4]float64 `json:"points"`
	}{info, out})
}

func (s *Server) deleteRoute(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	if snap := s.Session.Snapshot(); snap.Route != nil && snap.Route.ID == id {
		s.Session.SelectRoute(nil)
	}
	if err := s.Store.DeleteRoute(id); err != nil {
		storeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) selectRoute(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ID *int64 `json:"id"`
	}
	if !decode(w, r, &req) {
		return
	}
	if err := s.Session.SelectRoute(req.ID); err != nil {
		storeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) listRides(w http.ResponseWriter, r *http.Request) {
	rides, err := s.Store.Rides()
	if err != nil {
		storeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, rides)
}

func (s *Server) rideSamples(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	samples, err := s.Store.Samples(id)
	if err != nil {
		storeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, samples)
}

func (s *Server) uploadRide(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var req struct {
		Name string `json:"name"`
	}
	if r.ContentLength != 0 && !decode(w, r, &req) {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Minute)
	defer cancel()
	url, err := s.Uploader.Strava(ctx, id, strings.TrimSpace(req.Name))
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, err)
	case errors.Is(err, strava.ErrNotLoggedIn):
		writeError(w, http.StatusBadRequest, err)
	case err != nil:
		writeError(w, http.StatusBadGateway, err)
	default:
		writeJSON(w, http.StatusOK, map[string]string{"url": url})
	}
}

func (s *Server) deleteRide(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	if snap := s.Session.Snapshot(); snap.Ride.Recording && snap.Ride.ID == id {
		writeError(w, http.StatusConflict, errors.New("finish the ride before deleting it"))
		return
	}
	if err := s.Store.DeleteRide(id); err != nil {
		storeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// staticHandler serves the embedded build, falling back to index.html.
func staticHandler() http.Handler {
	dist, err := fs.Sub(web.Dist, "dist")
	if err != nil {
		panic(err)
	}
	files := http.FileServerFS(dist)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
		if p == "" {
			p = "index.html"
		}
		if _, err := fs.Stat(dist, p); err != nil {
			r.URL.Path = "/"
			p = "index.html"
		}
		if strings.HasPrefix(p, "assets/") {
			// Vite puts content hashes in asset names.
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		} else {
			w.Header().Set("Cache-Control", "no-cache")
		}
		files.ServeHTTP(w, r)
	})
}
