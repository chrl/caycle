package strava

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
)

// NewClient returns a client for the given API application whose
// credentials will be saved to path once logged in.
func NewClient(path, clientID, clientSecret string) *Client {
	c := newClient(path)
	c.Creds.ClientID = clientID
	c.Creds.ClientSecret = clientSecret
	return c
}

// Login runs the OAuth flow: it starts a local callback server, asks the user
// to authorize the app in the browser (via openURL), exchanges the code for
// tokens and saves them.
func (c *Client) Login(ctx context.Context, openURL func(string)) error {
	// Strava whitelists 127.0.0.1 as a callback domain for every app.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return err
	}
	defer ln.Close()
	redirect := fmt.Sprintf("http://%s/callback", ln.Addr())

	stateBytes := make([]byte, 16)
	rand.Read(stateBytes)
	state := hex.EncodeToString(stateBytes)

	type result struct {
		code string
		err  error
	}
	results := make(chan result, 1)
	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/callback" {
			http.NotFound(w, r)
			return
		}
		q := r.URL.Query()
		var res result
		switch {
		case q.Get("state") != state:
			res.err = errors.New("OAuth state mismatch")
		case q.Get("error") != "":
			res.err = fmt.Errorf("authorization denied: %s", q.Get("error"))
		case !strings.Contains(q.Get("scope"), "activity:write"):
			res.err = errors.New(`permission to upload activities was not granted; tick "Upload your activities" when authorizing`)
		default:
			res.code = q.Get("code")
		}
		if res.err != nil {
			fmt.Fprintf(w, "caycle: %v. You can close this tab.", res.err)
		} else {
			fmt.Fprint(w, "caycle is connected to Strava. You can close this tab.")
		}
		select {
		case results <- res:
		default:
		}
	})}
	go srv.Serve(ln)
	defer srv.Close()

	authURL := c.OAuthURL + "/authorize?" + url.Values{
		"client_id":       {c.Creds.ClientID},
		"redirect_uri":    {redirect},
		"response_type":   {"code"},
		"approval_prompt": {"auto"},
		"scope":           {"activity:write"},
		"state":           {state},
	}.Encode()
	openURL(authURL)

	var res result
	select {
	case res = <-results:
	case <-ctx.Done():
		return ctx.Err()
	}
	if res.err != nil {
		return res.err
	}
	return c.exchange(ctx, url.Values{
		"grant_type": {"authorization_code"},
		"code":       {res.code},
	})
}
