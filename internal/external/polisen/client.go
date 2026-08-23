package polisen

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/backflash-cli/backflash/internal/external"
)

const Endpoint = "https://polisen.se/api/events"

type PermanentError struct{ Message string }

func (e PermanentError) Error() string   { return e.Message }
func (e PermanentError) Permanent() bool { return true }

type Client struct {
	HTTP      *http.Client
	Limiter   *external.RateLimiter
	UserAgent string
	Now       func() time.Time
}

func NewClient(httpClient *http.Client, limiter *external.RateLimiter) *Client {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 30 * time.Second}
	}
	if limiter == nil {
		limiter = external.NewRateLimiter()
	}
	return &Client{HTTP: httpClient, Limiter: limiter, UserAgent: "BACKFLASH/0.1 (+https://github.com/backflash-cli/backflash)", Now: time.Now}
}
func (c *Client) Source() string { return Source }
func (c *Client) Fetch(ctx context.Context) ([]external.ExternalEvent, error) {
	if err := c.Limiter.Wait(ctx); err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, Endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", c.UserAgent)
	req.Header.Set("Accept", "application/json")
	res, err := c.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode == http.StatusNotFound {
		return nil, PermanentError{"Polisens händelse-API finns inte längre (404)"}
	}
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return nil, fmt.Errorf("Polisens API svarade med HTTP %s", res.Status)
	}
	body, err := io.ReadAll(res.Body)
	if err != nil {
		return nil, err
	}
	if !json.Valid(body) {
		return nil, fmt.Errorf("Polisens API svarade inte med giltig JSON")
	}
	return Parse(body, c.Now())
}
