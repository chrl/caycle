package strava

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeStrava implements the token and upload endpoints.
type fakeStrava struct {
	t        *testing.T
	mu       sync.Mutex
	grants   []string
	uploaded string // file content
	fields   url.Values
	polls    int
	reject   string // upload error to return, if any
}

func (f *fakeStrava) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	switch {
	case r.Method == http.MethodPost && r.URL.Path == "/oauth/token":
		r.ParseForm()
		if r.Form.Get("client_id") != "id" || r.Form.Get("client_secret") != "secret" {
			http.Error(w, `{"message":"Bad Request"}`, http.StatusBadRequest)
			return
		}
		f.grants = append(f.grants, r.Form.Get("grant_type"))
		json.NewEncoder(w).Encode(map[string]any{
			"access_token":  "access-" + r.Form.Get("grant_type"),
			"refresh_token": "refresh",
			"expires_at":    time.Now().Add(6 * time.Hour).Unix(),
			"athlete":       map[string]string{"firstname": "Ada", "lastname": "Rider"},
		})
	case r.Method == http.MethodPost && r.URL.Path == "/api/v3/uploads":
		if r.Header.Get("Authorization") != "Bearer access-refresh_token" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			f.t.Errorf("parse multipart: %v", err)
		}
		f.fields = r.MultipartForm.Value
		file, _, err := r.FormFile("file")
		if err != nil {
			f.t.Errorf("no file: %v", err)
			return
		}
		data, _ := io.ReadAll(file)
		f.uploaded = string(data)
		if f.reject != "" {
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]any{"id_str": "", "error": f.reject})
			return
		}
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(map[string]any{"id_str": "42", "status": "Your activity is still being processed.", "activity_id": nil})
	case r.Method == http.MethodGet && r.URL.Path == "/api/v3/uploads/42":
		f.polls++
		var activity any
		if f.polls >= 2 {
			activity = 1234
		}
		json.NewEncoder(w).Encode(map[string]any{"id_str": "42", "status": "processing", "activity_id": activity})
	default:
		http.NotFound(w, r)
	}
}

func testClient(t *testing.T, srv *httptest.Server, c *Client) *Client {
	c.OAuthURL = srv.URL + "/oauth"
	c.APIURL = srv.URL + "/api/v3"
	c.PollInterval = time.Millisecond
	return c
}

func TestLoginRefreshAndUpload(t *testing.T) {
	fake := &fakeStrava{t: t}
	srv := httptest.NewServer(fake)
	defer srv.Close()
	dir := t.TempDir()
	credPath := filepath.Join(dir, "strava.json")

	c := testClient(t, srv, NewClient(credPath, "id", "secret"))
	// Play the browser: approve and follow the redirect back to caycle.
	approve := func(authURL string) {
		u, _ := url.Parse(authURL)
		q := u.Query()
		if q.Get("scope") != "activity:write" {
			t.Errorf("scope = %q", q.Get("scope"))
		}
		cb := q.Get("redirect_uri") + "?" + url.Values{
			"state": {q.Get("state")},
			"code":  {"the-code"},
			"scope": {"read,activity:write"},
		}.Encode()
		go func() {
			resp, err := http.Get(cb)
			if err != nil {
				t.Errorf("callback: %v", err)
				return
			}
			resp.Body.Close()
		}()
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := c.Login(ctx, approve); err != nil {
		t.Fatalf("login: %v", err)
	}

	info, err := os.Stat(credPath)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("credentials file mode = %v, want 0600", info.Mode().Perm())
	}

	// Load stored credentials and force a refresh by expiring the token.
	c, err = Load(credPath)
	if err != nil {
		t.Fatal(err)
	}
	if c.Creds.Athlete != "Ada Rider" {
		t.Errorf("athlete = %q", c.Creds.Athlete)
	}
	c.Creds.ExpiresAt = time.Now().Unix()
	testClient(t, srv, c)

	ride := filepath.Join(dir, "ride.tcx")
	os.WriteFile(ride, []byte("<tcx/>"), 0o644)
	activity, err := c.Upload(ctx, ride, UploadOptions{Name: "Vortex session", Trainer: true})
	if err != nil {
		t.Fatalf("upload: %v", err)
	}
	if activity != "https://www.strava.com/activities/1234" {
		t.Errorf("activity URL = %q", activity)
	}
	if strings.Join(fake.grants, ",") != "authorization_code,refresh_token" {
		t.Errorf("grants = %v", fake.grants)
	}
	if fake.uploaded != "<tcx/>" {
		t.Errorf("uploaded %q", fake.uploaded)
	}
	for k, want := range map[string]string{"data_type": "tcx", "trainer": "1", "name": "Vortex session", "external_id": "ride.tcx"} {
		if got := fake.fields.Get(k); got != want {
			t.Errorf("field %s = %q, want %q", k, got, want)
		}
	}
	if _, ok := fake.fields["description"]; ok {
		t.Error("empty description should not be sent")
	}
}

func TestUploadRejected(t *testing.T) {
	fake := &fakeStrava{t: t, reject: "ride.tcx duplicate of activity 99"}
	srv := httptest.NewServer(fake)
	defer srv.Close()
	c := testClient(t, srv, NewClient(filepath.Join(t.TempDir(), "s.json"), "id", "secret"))
	c.Creds.RefreshToken = "refresh"

	ride := filepath.Join(t.TempDir(), "ride.tcx")
	os.WriteFile(ride, []byte("<tcx/>"), 0o644)
	_, err := c.Upload(context.Background(), ride, UploadOptions{})
	if err == nil || !strings.Contains(err.Error(), "duplicate of activity 99") {
		t.Fatalf("err = %v", err)
	}
}

func TestLoadNotLoggedIn(t *testing.T) {
	if _, err := Load(filepath.Join(t.TempDir(), "missing.json")); err != ErrNotLoggedIn {
		t.Fatalf("err = %v", err)
	}
}
