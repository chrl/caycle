// Package strava uploads activity files to Strava using the user's own API
// application (https://www.strava.com/settings/api).
package strava

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const (
	defaultOAuthURL = "https://www.strava.com/oauth"
	defaultAPIURL   = "https://www.strava.com/api/v3"
)

// ErrNotLoggedIn is returned when no credentials are stored.
var ErrNotLoggedIn = errors.New("not connected to Strava; run `caycle strava login`")

// Credentials are persisted between runs.
type Credentials struct {
	ClientID     string `json:"client_id"`
	ClientSecret string `json:"client_secret"`
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresAt    int64  `json:"expires_at"`
	Athlete      string `json:"athlete,omitempty"`
}

// Client talks to the Strava API, refreshing and saving tokens as needed.
type Client struct {
	Creds Credentials
	Path  string // where Creds are saved; empty keeps them in memory only

	HTTP     *http.Client
	OAuthURL string
	APIURL   string
	// PollInterval is the delay between upload status checks.
	PollInterval time.Duration
}

// Load reads stored credentials. It returns ErrNotLoggedIn if there are none.
func Load(path string) (*Client, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, ErrNotLoggedIn
	}
	if err != nil {
		return nil, err
	}
	c := newClient(path)
	if err := json.Unmarshal(data, &c.Creds); err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	if c.Creds.RefreshToken == "" {
		return nil, ErrNotLoggedIn
	}
	return c, nil
}

func newClient(path string) *Client {
	return &Client{
		Path:         path,
		HTTP:         &http.Client{Timeout: 60 * time.Second},
		OAuthURL:     defaultOAuthURL,
		APIURL:       defaultAPIURL,
		PollInterval: 2 * time.Second,
	}
}

func (c *Client) save() error {
	if c.Path == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(c.Path), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(c.Creds, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(c.Path, data, 0o600)
}

type tokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresAt    int64  `json:"expires_at"`
	Athlete      *struct {
		Firstname string `json:"firstname"`
		Lastname  string `json:"lastname"`
	} `json:"athlete"`
}

// exchange posts to the token endpoint and stores the resulting tokens.
func (c *Client) exchange(ctx context.Context, form url.Values) error {
	form.Set("client_id", c.Creds.ClientID)
	form.Set("client_secret", c.Creds.ClientSecret)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.OAuthURL+"/token", strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	var tok tokenResponse
	if err := c.do(req, &tok); err != nil {
		return fmt.Errorf("token request: %w", err)
	}
	c.Creds.AccessToken = tok.AccessToken
	c.Creds.RefreshToken = tok.RefreshToken
	c.Creds.ExpiresAt = tok.ExpiresAt
	if tok.Athlete != nil {
		c.Creds.Athlete = strings.TrimSpace(tok.Athlete.Firstname + " " + tok.Athlete.Lastname)
	}
	return c.save()
}

// Refresh renews the access token now, which also proves the stored login
// still works.
func (c *Client) Refresh(ctx context.Context) error {
	return c.exchange(ctx, url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {c.Creds.RefreshToken},
	})
}

// token returns a valid access token, refreshing it if it expires soon.
func (c *Client) token(ctx context.Context) (string, error) {
	if time.Now().Add(time.Minute).Unix() < c.Creds.ExpiresAt {
		return c.Creds.AccessToken, nil
	}
	err := c.exchange(ctx, url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {c.Creds.RefreshToken},
	})
	if err != nil {
		return "", err
	}
	return c.Creds.AccessToken, nil
}

// do sends a request and decodes a JSON response, turning non-2xx responses
// into errors that carry Strava's message.
func (c *Client) do(req *http.Request, out any) error {
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if resp.StatusCode/100 != 2 {
		// Upload errors come back as an upload object with "error" set.
		var u uploadResponse
		if json.Unmarshal(body, &u) == nil && u.Error != "" {
			return errors.New(u.Error)
		}
		return fmt.Errorf("%s: %s", resp.Status, bytes.TrimSpace(body))
	}
	return json.Unmarshal(body, out)
}

// UploadOptions are optional activity fields.
type UploadOptions struct {
	Name        string // empty lets Strava pick one, e.g. "Evening Ride"
	Description string
	ExternalID  string // defaults to the file name
	SportType   string // e.g. "VirtualRide"; empty uses the file's sport
	// Trainer marks the activity as an indoor trainer ride. Strava hides the
	// map of trainer rides, so leave it off for rides with a virtual route.
	Trainer bool
}

type uploadResponse struct {
	IDStr      string `json:"id_str"`
	Error      string `json:"error"`
	Status     string `json:"status"`
	ActivityID *int64 `json:"activity_id"`
}

// Upload sends a TCX file, waits for Strava to process it and returns the new
// activity's URL.
func (c *Client) Upload(ctx context.Context, path string, opts UploadOptions) (string, error) {
	tok, err := c.token(ctx)
	if err != nil {
		return "", err
	}
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	fields := map[string]string{
		"data_type":   strings.TrimPrefix(filepath.Ext(path), "."),
		"name":        opts.Name,
		"description": opts.Description,
		"external_id": opts.ExternalID,
		"sport_type":  opts.SportType,
	}
	if opts.Trainer {
		fields["trainer"] = "1"
	}
	if fields["external_id"] == "" {
		fields["external_id"] = filepath.Base(path)
	}
	for k, v := range fields {
		if v != "" {
			mw.WriteField(k, v)
		}
	}
	fw, err := mw.CreateFormFile("file", filepath.Base(path))
	if err != nil {
		return "", err
	}
	if _, err := io.Copy(fw, f); err != nil {
		return "", err
	}
	if err := mw.Close(); err != nil {
		return "", err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.APIURL+"/uploads", &body)
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+tok)
	var up uploadResponse
	if err := c.do(req, &up); err != nil {
		return "", fmt.Errorf("upload: %w", err)
	}
	return c.wait(ctx, tok, up)
}

// wait polls the upload until Strava has created the activity.
func (c *Client) wait(ctx context.Context, tok string, up uploadResponse) (string, error) {
	for {
		if up.Error != "" {
			return "", fmt.Errorf("strava rejected the upload: %s", up.Error)
		}
		if up.ActivityID != nil {
			return "https://www.strava.com/activities/" + strconv.FormatInt(*up.ActivityID, 10), nil
		}
		select {
		case <-ctx.Done():
			return "", fmt.Errorf("upload %s still processing (%s): %w", up.IDStr, up.Status, ctx.Err())
		case <-time.After(c.PollInterval):
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.APIURL+"/uploads/"+up.IDStr, nil)
		if err != nil {
			return "", err
		}
		req.Header.Set("Authorization", "Bearer "+tok)
		if err := c.do(req, &up); err != nil {
			return "", fmt.Errorf("upload status: %w", err)
		}
	}
}
