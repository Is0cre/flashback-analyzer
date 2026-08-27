package geo

import (
	"context"
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestDistanceKMMatchesKnownCityPair(t *testing.T) {
	stockholm := Location{Latitude: 59.33, Longitude: 18.06}
	gothenburg := Location{Latitude: 57.71, Longitude: 11.97}
	got := DistanceKM(stockholm, gothenburg)
	if math.Abs(got-397) > 15 {
		t.Fatalf("Stockholm-Göteborg = %.1f km, väntade ~397 km", got)
	}
	if d := DistanceKM(stockholm, stockholm); d != 0 {
		t.Fatalf("avståndet mellan en punkt och sig själv ska vara 0, fick %.4f", d)
	}
}

func TestLocateParsesCoordinatesFromResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"latitude": 59.33, "longitude": 18.06})
	}))
	defer server.Close()

	client := NewClient(server.Client())
	client.Endpoint = server.URL
	got, err := client.Locate(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got.Latitude != 59.33 || got.Longitude != 18.06 {
		t.Fatalf("fel koordinater: %+v", got)
	}
}

func TestLocateReportsAPIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"error": true, "reason": "Reserved IP Address"})
	}))
	defer server.Close()

	client := NewClient(server.Client())
	client.Endpoint = server.URL
	if _, err := client.Locate(context.Background()); err == nil {
		t.Fatal("väntade ett fel för en API-rapporterad platsuppslagning som misslyckats")
	}
}

func TestLocateReportsHTTPErrorStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer server.Close()

	client := NewClient(server.Client())
	client.Endpoint = server.URL
	if _, err := client.Locate(context.Background()); err == nil {
		t.Fatal("väntade ett fel för HTTP 429")
	}
}
