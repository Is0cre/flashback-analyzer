// Package geo resolves an approximate location for the machine's public IP
// address, for the police-events proximity alarm. It never touches the
// network unless a caller explicitly asks for it (Client.Locate) — nothing
// in this package runs automatically.
package geo

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"time"
)

// Location is an approximate lat/lon in decimal degrees.
type Location struct {
	Latitude  float64
	Longitude float64
}

// DefaultEndpoint is a free, no-API-key IP geolocation lookup. It reports
// the location of whatever IP the request arrives from, so no coordinates
// or address are ever sent — only an outbound HTTPS request is made.
const DefaultEndpoint = "https://ipapi.co/json/"

// Client resolves a Location from the caller's public IP. It mirrors the
// shape of the Polisen client (internal/external/polisen/client.go):
// injected *http.Client, fixed user-agent, context-aware, JSON body
// validated before parsing.
type Client struct {
	HTTP      *http.Client
	UserAgent string
	// Endpoint is overridable so tests can point at an httptest server
	// instead of the real IP-geolocation service.
	Endpoint string
}

func NewClient(httpClient *http.Client) *Client {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 10 * time.Second}
	}
	return &Client{
		HTTP:      httpClient,
		UserAgent: "BACKFLASH/0.1 (+https://github.com/backflash-cli/backflash)",
		Endpoint:  DefaultEndpoint,
	}
}

type apiResponse struct {
	Latitude  float64 `json:"latitude"`
	Longitude float64 `json:"longitude"`
	Error     bool    `json:"error"`
	Reason    string  `json:"reason"`
}

// Locate resolves an approximate location for the caller's current public
// IP address. It is a best-effort, one-shot lookup: callers should treat a
// non-nil error as "proximity alerts unavailable this run" rather than
// something to retry aggressively, since it depends on a free third-party
// API with its own uptime and rate limits.
func (c *Client) Locate(ctx context.Context) (Location, error) {
	endpoint := c.Endpoint
	if endpoint == "" {
		endpoint = DefaultEndpoint
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return Location{}, err
	}
	req.Header.Set("User-Agent", c.UserAgent)
	req.Header.Set("Accept", "application/json")
	res, err := c.HTTP.Do(req)
	if err != nil {
		return Location{}, err
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return Location{}, fmt.Errorf("platstjänsten svarade med HTTP %s", res.Status)
	}
	body, err := io.ReadAll(res.Body)
	if err != nil {
		return Location{}, err
	}
	if !json.Valid(body) {
		return Location{}, fmt.Errorf("platstjänsten svarade inte med giltig JSON")
	}
	var parsed apiResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return Location{}, err
	}
	if parsed.Error {
		return Location{}, fmt.Errorf("platstjänsten kunde inte slå upp platsen: %s", parsed.Reason)
	}
	if parsed.Latitude == 0 && parsed.Longitude == 0 {
		return Location{}, fmt.Errorf("platstjänsten returnerade inga koordinater")
	}
	return Location{Latitude: parsed.Latitude, Longitude: parsed.Longitude}, nil
}

// DistanceKM returns the great-circle distance between two points in
// kilometers using the haversine formula — accurate enough at proximity-alert
// scale (single-digit to a few hundred km) without needing an ellipsoidal
// model.
func DistanceKM(a, b Location) float64 {
	const earthRadiusKM = 6371.0
	lat1, lat2 := a.Latitude*math.Pi/180, b.Latitude*math.Pi/180
	dLat := (b.Latitude - a.Latitude) * math.Pi / 180
	dLon := (b.Longitude - a.Longitude) * math.Pi / 180
	h := math.Sin(dLat/2)*math.Sin(dLat/2) + math.Cos(lat1)*math.Cos(lat2)*math.Sin(dLon/2)*math.Sin(dLon/2)
	return 2 * earthRadiusKM * math.Asin(math.Sqrt(h))
}
