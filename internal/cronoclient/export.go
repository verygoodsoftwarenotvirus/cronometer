package cronoclient

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"time"
)

// dateFormat is the YYYY-MM-DD form Cronometer's export endpoint expects.
const dateFormat = "2006-01-02"

// export performs a generic CSV export for the given "generate" kind over the inclusive date
// range. Only the calendar date of start/end is used. The returned string is raw CSV.
func (c *Client) export(ctx context.Context, kind string, start, end time.Time) (result string, err error) {
	token, err := c.generateAuthToken(ctx)
	if err != nil {
		return "", fmt.Errorf("minting export token: %w", err)
	}

	req, err := c.newExportRequest(ctx, http.MethodGet, apiExportURL, http.NoBody)
	if err != nil {
		return "", fmt.Errorf("building %s export request: %w", kind, err)
	}

	q := req.URL.Query()
	q.Set("nonce", token)
	q.Set("generate", kind)
	q.Set("start", start.Format(dateFormat))
	q.Set("end", end.Format(dateFormat))
	req.URL.RawQuery = q.Encode()

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("executing %s export request: %w", kind, err)
	}
	defer func() { err = closeBody(resp.Body, err) }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("reading %s export response: %w", kind, err)
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("%s export: unexpected status %d: %s", kind, resp.StatusCode, string(body))
	}

	return string(body), nil
}

// ExportDailyNutrition returns the raw CSV of per-day nutrient totals (Cronometer's "daily
// summary") over the inclusive date range.
func (c *Client) ExportDailyNutrition(ctx context.Context, start, end time.Time) (string, error) {
	return c.export(ctx, "dailySummary", start, end)
}

// Future scope (v1 is summary-only): the same export() helper backs servings, exercises,
// biometrics, and notes via the "servings", "exercises", "biometrics", and "notes" kinds.
