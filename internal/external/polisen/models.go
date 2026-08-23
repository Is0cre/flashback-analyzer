package polisen

import "encoding/json"

type apiEvent struct {
	ID       json.RawMessage `json:"id"`
	Datetime string          `json:"datetime"`
	Name     string          `json:"name"`
	Summary  string          `json:"summary"`
	URL      string          `json:"url"`
	Type     string          `json:"type"`
	Location struct {
		Name string `json:"name"`
		GPS  string `json:"gps"`
	} `json:"location"`
}
