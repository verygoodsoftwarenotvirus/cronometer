// Package cronoclient is a minimal client for Cronometer's unofficial, reverse-engineered
// GWT-RPC + CSV-export API — the same endpoints the web app uses internally. There is no
// official Cronometer API.
//
// This package is adapted from github.com/burke/gocronometer (and the original
// github.com/jrmycanady/gocronometer), which are licensed under the GNU GPL v2. As a
// derivative, this project is likewise GPL-2.0; see the repository LICENSE.
package cronoclient

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"time"
)

// Cronometer endpoint URLs.
const (
	htmlLoginURL = "https://cronometer.com/login/"
	apiLoginURL  = "https://cronometer.com/login"
	gwtBaseURL   = "https://cronometer.com/cronometer/app"
	apiExportURL = "https://cronometer.com/export"
)

// GWT "magic" header values, captured by inspecting a real request from the web app.
//
// gwtPermutation and gwtHeader change whenever Cronometer ships a web-app update — they appear
// to pin the expected client version. They are THE FIRST THING TO UPDATE when auth suddenly
// starts failing: open the web app with devtools, find a request to /cronometer/app, and copy
// the x-gwt-permutation header and the leading hash in the request body. Both can be overridden
// at runtime via ClientOptions without recompiling.
const (
	defaultGWTContentType = "text/x-gwt-rpc; charset=UTF-8"
	defaultGWTModuleBase  = "https://cronometer.com/cronometer/"
	defaultGWTPermutation = "7B121DC5483BF272B1BC1916DA9FA963"
	defaultGWTHeader      = "2D6A926E3729946302DC68073CB0D550"
)

// Client talks to Cronometer's unofficial API. Construct one with NewClient; the zero value is
// not usable.
type Client struct {
	httpClient *http.Client

	// nonce is the session nonce (sesnonce cookie) captured during login; userID is resolved
	// from the GWT authenticate call. Both are needed to mint export tokens.
	nonce  string
	userID string

	gwtContentType string
	gwtModuleBase  string
	gwtPermutation string
	gwtHeader      string
}

// ClientOptions overrides the GWT magic values. Zero-value fields fall back to the package
// defaults. Use this to patch a broken permutation/header without rebuilding.
type ClientOptions struct {
	GWTContentType string
	GWTModuleBase  string
	GWTPermutation string
	GWTHeader      string
}

// NewClient returns a Client with a cookie jar (required to carry the session) and sensible
// defaults. opts may be nil.
func NewClient(opts *ClientOptions) *Client {
	jar, err := cookiejar.New(nil)
	if err != nil {
		// cookiejar.New(nil) never returns an error in the standard library; this guards an
		// impossible case rather than silently dropping the jar.
		panic(fmt.Sprintf("cronoclient: cookiejar.New: %v", err))
	}

	c := &Client{
		httpClient: &http.Client{
			Jar:     jar,
			Timeout: 60 * time.Second,
		},
		gwtContentType: defaultGWTContentType,
		gwtModuleBase:  defaultGWTModuleBase,
		gwtPermutation: defaultGWTPermutation,
		gwtHeader:      defaultGWTHeader,
	}

	if opts != nil {
		if opts.GWTContentType != "" {
			c.gwtContentType = opts.GWTContentType
		}
		if opts.GWTModuleBase != "" {
			c.gwtModuleBase = opts.GWTModuleBase
		}
		if opts.GWTPermutation != "" {
			c.gwtPermutation = opts.GWTPermutation
		}
		if opts.GWTHeader != "" {
			c.gwtHeader = opts.GWTHeader
		}
	}

	return c
}

// newGWTRequest builds a request carrying the GWT-RPC headers.
func (c *Client) newGWTRequest(ctx context.Context, method, reqURL string, body io.Reader) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, method, reqURL, body)
	if err != nil {
		return nil, err
	}

	req.Header.Set("content-type", c.gwtContentType)
	req.Header.Set("x-gwt-module-base", c.gwtModuleBase)
	req.Header.Set("x-gwt-permutation", c.gwtPermutation)

	return req, nil
}

// newExportRequest builds a request that mimics a same-origin document navigation, as the
// export endpoint expects.
func (c *Client) newExportRequest(ctx context.Context, method, reqURL string, body io.Reader) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, method, reqURL, body)
	if err != nil {
		return nil, err
	}

	req.Header.Set("sec-fetch-dest", "document")
	req.Header.Set("sec-fetch-mode", "navigate")
	req.Header.Set("sec-fetch-site", "same-origin")

	return req, nil
}

// Session is the durable authentication state (the 14-day sesnonce plus the resolved user ID).
// Persist it to skip re-login — and re-running 2FA — until it expires.
type Session struct {
	Nonce  string `json:"nonce"`
	UserID string `json:"user_id"`
}

// Session returns the current authentication state for persistence.
func (c *Client) Session() Session {
	return Session{Nonce: c.nonce, UserID: c.userID}
}

// HasSession reports whether the client holds session state (restored or freshly logged in).
func (c *Client) HasSession() bool {
	return c.nonce != "" && c.userID != ""
}

// RestoreSession loads a previously persisted session: it sets the nonce and user ID and seeds
// the cookie jar with the sesnonce cookie so GWT/export requests carry it. The session may still
// be expired server-side; callers should fall back to Login if a subsequent request fails.
func (c *Client) RestoreSession(s Session) {
	c.nonce = s.Nonce
	c.userID = s.UserID
	if u, err := url.Parse(defaultGWTModuleBase); err == nil {
		c.httpClient.Jar.SetCookies(u, []*http.Cookie{{Name: "sesnonce", Value: s.Nonce}})
	}
}

// closeBody closes rc and folds any close error into a named return: it surfaces the close
// error only when no prior error occurred. Use it as: defer func() { err = closeBody(body, err) }().
func closeBody(rc io.Closer, prior error) error {
	if cerr := rc.Close(); cerr != nil && prior == nil {
		return fmt.Errorf("closing response body: %w", cerr)
	}
	return prior
}
