package cronoclient

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"

	"golang.org/x/net/html"
)

// gwtTokenRegex pulls the quoted token out of a generateAuthorizationToken response.
var gwtTokenRegex = regexp.MustCompile(`"(.*)"`)

// gwtAuthRegex pulls the user ID out of an authenticate response of the form OK[<userid>,...].
var gwtAuthRegex = regexp.MustCompile(`OK\[(\d*),.*`)

// loginResponse is the JSON returned by the form login endpoint.
type loginResponse struct {
	Redirect string `json:"redirect"`
	Error    string `json:"error"`
	Success  bool   `json:"success"`
}

// gwtGenerateAuthTokenBody returns the GWT-RPC body for minting an export authorization token.
// The two format verbs are the session nonce and the user ID.
func (c *Client) gwtGenerateAuthTokenBody() string {
	return "7|0|8|" + c.gwtModuleBase + "|" + c.gwtHeader +
		"|com.cronometer.shared.rpc.CronometerService|generateAuthorizationToken" +
		"|java.lang.String/2004016611|I|com.cronometer.shared.user.AuthScope/2065601159|%s|1|2|3|4|4|5|6|6|7|8|%s|3600|7|2|"
}

// gwtAuthenticateBody returns the GWT-RPC body for the authenticate call.
func (c *Client) gwtAuthenticateBody() string {
	return "7|0|5|" + c.gwtModuleBase + "|" + c.gwtHeader +
		"|com.cronometer.shared.rpc.CronometerService|authenticate|java.lang.Integer/3438268394|1|2|3|4|1|5|5|-300|"
}

// Login authenticates with Cronometer using email/password, captures the session nonce, and
// completes GWT authentication. It must be called before any export.
//
// userCode is the 6-digit two-factor (TOTP) code; pass "" when the account has 2FA disabled.
// Cronometer takes the code as a plain field on the login form, so no separate challenge step is
// needed — success is signaled by the server issuing a sesnonce cookie.
func (c *Client) Login(ctx context.Context, email, password, userCode string) (err error) {
	antiCSRF, err := c.obtainAntiCSRF(ctx)
	if err != nil {
		return fmt.Errorf("obtaining anti-CSRF token: %w", err)
	}

	form := url.Values{}
	form.Set("username", email)
	form.Set("password", password)
	form.Set("userCode", userCode)
	form.Set("anticsrf", antiCSRF)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, apiLoginURL, strings.NewReader(form.Encode()))
	if err != nil {
		return fmt.Errorf("building login request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("X-Requested-With", "XMLHttpRequest")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("executing login request: %w", err)
	}
	defer func() { err = closeBody(resp.Body, err) }()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("login: unexpected status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("reading login response: %w", err)
	}

	// The body is JSON when present; surface its error message if we can parse one. It may also
	// be empty, so a parse failure is not itself fatal — the sesnonce cookie is the source of
	// truth for success.
	var lr loginResponse
	if jsonErr := json.Unmarshal(body, &lr); jsonErr == nil && lr.Error != "" {
		return fmt.Errorf("login rejected: %s", lr.Error)
	}

	c.captureNonce(resp)
	if c.nonce == "" {
		return fmt.Errorf("login did not establish a session; check credentials and 2FA code")
	}

	if err = c.gwtAuthenticate(ctx); err != nil {
		return fmt.Errorf("authenticating with GWT: %w", err)
	}

	return nil
}

// obtainAntiCSRF fetches the login page and extracts the hidden anticsrf form value.
func (c *Client) obtainAntiCSRF(ctx context.Context) (csrf string, err error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, htmlLoginURL, http.NoBody)
	if err != nil {
		return "", fmt.Errorf("building login-page request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("fetching login page: %w", err)
	}
	defer func() { err = closeBody(resp.Body, err) }()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("login page: unexpected status %d", resp.StatusCode)
	}

	root, err := html.Parse(resp.Body)
	if err != nil {
		return "", fmt.Errorf("parsing login page HTML: %w", err)
	}

	csrf = findAntiCSRF(root)
	if csrf == "" {
		return "", fmt.Errorf("anticsrf input not found on login page")
	}

	return csrf, nil
}

// findAntiCSRF walks the parsed HTML for <input name="anticsrf" value="...">.
func findAntiCSRF(n *html.Node) string {
	if n.Type == html.ElementNode && n.Data == "input" {
		var name, value string
		for _, attr := range n.Attr {
			switch attr.Key {
			case "name":
				name = attr.Val
			case "value":
				value = attr.Val
			}
		}
		if name == "anticsrf" {
			return value
		}
	}

	for child := n.FirstChild; child != nil; child = child.NextSibling {
		if v := findAntiCSRF(child); v != "" {
			return v
		}
	}

	return ""
}

// gwtAuthenticate completes GWT authentication using the session nonce and records the user ID.
func (c *Client) gwtAuthenticate(ctx context.Context) (err error) {
	req, err := c.newGWTRequest(ctx, http.MethodPost, gwtBaseURL, strings.NewReader(c.gwtAuthenticateBody()))
	if err != nil {
		return fmt.Errorf("building GWT authenticate request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("executing GWT authenticate request: %w", err)
	}
	defer func() { err = closeBody(resp.Body, err) }()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("GWT authenticate: unexpected status %d", resp.StatusCode)
	}

	c.captureNonce(resp)

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("reading GWT authenticate response: %w", err)
	}

	match := gwtAuthRegex.FindStringSubmatch(string(body))
	if len(match) != 2 {
		return fmt.Errorf("user ID not found in GWT authenticate response")
	}
	c.userID = match[1]

	return nil
}

// generateAuthToken mints a short-lived token used as the nonce for export requests.
func (c *Client) generateAuthToken(ctx context.Context) (token string, err error) {
	reqBody := fmt.Sprintf(c.gwtGenerateAuthTokenBody(), c.nonce, c.userID)

	req, err := c.newGWTRequest(ctx, http.MethodPost, gwtBaseURL, strings.NewReader(reqBody))
	if err != nil {
		return "", fmt.Errorf("building auth-token request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("executing auth-token request: %w", err)
	}
	defer func() { err = closeBody(resp.Body, err) }()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("auth-token: unexpected status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("reading auth-token response: %w", err)
	}

	match := gwtTokenRegex.FindStringSubmatch(string(body))
	if len(match) != 2 {
		return "", fmt.Errorf("token not found in auth-token response")
	}

	return match[1], nil
}

// captureNonce records the sesnonce cookie from a response, if present.
func (c *Client) captureNonce(resp *http.Response) {
	if resp == nil {
		return
	}
	for _, cookie := range resp.Cookies() {
		if cookie.Name == "sesnonce" {
			c.nonce = cookie.Value
		}
	}
}
