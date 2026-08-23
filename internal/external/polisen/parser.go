package polisen

import (
	"encoding/json"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/backflash-cli/backflash/internal/external"
)

const Source = "polisen"

var namedEventTime = regexp.MustCompile(`(?i)^\s*(\d{1,2})\s+(januari|februari|mars|april|maj|juni|juli|augusti|september|oktober|november|december)\s+(\d{1,2})[.:](\d{2})`)

func Parse(data []byte, now time.Time) ([]external.ExternalEvent, error) {
	var raw []apiEvent
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, err
	}
	result := make([]external.ExternalEvent, 0, len(raw))
	for _, item := range raw {
		eventTime, err := time.Parse(time.RFC3339, item.Datetime)
		if err != nil {
			eventTime = parseFlexibleTime(item.Datetime)
		}
		if eventTime.IsZero() {
			eventTime = parseNamedEventTime(item.Name, now)
		}
		lat, lon := parseGPS(item.Location.GPS)
		result = append(result, external.ExternalEvent{Source: Source, ExternalID: jsonValue(item.ID), Timestamp: eventTime, Title: strings.TrimSpace(item.Name), Summary: strings.TrimSpace(item.Summary), EventType: strings.TrimSpace(item.Type), LocationName: strings.TrimSpace(item.Location.Name), Latitude: lat, Longitude: lon, URL: normalizeURL(item.URL), FirstSeenAt: now, LastSeenAt: now})
	}
	return result, nil
}

func parseNamedEventTime(value string, now time.Time) time.Time {
	match := namedEventTime.FindStringSubmatch(strings.TrimSpace(value))
	if len(match) != 5 {
		return time.Time{}
	}
	months := map[string]time.Month{
		"januari": 1, "februari": 2, "mars": 3, "april": 4, "maj": 5, "juni": 6,
		"juli": 7, "augusti": 8, "september": 9, "oktober": 10, "november": 11, "december": 12,
	}
	month, ok := months[strings.ToLower(match[2])]
	if !ok {
		return time.Time{}
	}
	day, _ := strconv.Atoi(match[1])
	hour, _ := strconv.Atoi(match[3])
	minute, _ := strconv.Atoi(match[4])
	if day < 1 || hour > 23 || minute > 59 {
		return time.Time{}
	}
	location := now.Location()
	result := time.Date(now.Year(), month, day, hour, minute, 0, 0, location)
	if result.After(now.Add(24 * time.Hour)) {
		result = result.AddDate(-1, 0, 0)
	}
	return result
}

func parseFlexibleTime(value string) time.Time {
	for _, layout := range []string{time.RFC3339Nano, "2006-01-02 15:04:05 -0700", "2006-01-02 15:04:05", "2006-01-02 15:04"} {
		if t, err := time.Parse(layout, strings.TrimSpace(value)); err == nil {
			return t
		}
	}
	return time.Time{}
}

func parseGPS(value string) (*float64, *float64) {
	parts := strings.Split(value, ",")
	if len(parts) != 2 {
		return nil, nil
	}
	lat, err1 := strconv.ParseFloat(strings.TrimSpace(parts[0]), 64)
	lon, err2 := strconv.ParseFloat(strings.TrimSpace(parts[1]), 64)
	if err1 != nil || err2 != nil {
		return nil, nil
	}
	return &lat, &lon
}

func normalizeURL(value string) string {
	u, err := url.Parse(strings.TrimSpace(value))
	if err != nil {
		return ""
	}
	if u.IsAbs() {
		return u.String()
	}
	base, _ := url.Parse("https://polisen.se/")
	return base.ResolveReference(u).String()
}

func jsonValue(raw json.RawMessage) string {
	var value string
	if json.Unmarshal(raw, &value) == nil {
		return value
	}
	return strings.Trim(string(raw), " \t\r\n")
}
