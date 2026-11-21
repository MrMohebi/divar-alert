package divar

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const (
	PlacesURL         = "https://api.divar.ir/v8/postlist/w/places"
	PlacesSourcePage  = "https://divar.ir/s/kerman"
	DefaultPlacesFile = "divar/places.json"
)

// Place preserves the country/province/city hierarchy and Divar's string IDs.
type Place struct {
	ID                    string  `json:"id"`
	Name                  string  `json:"name"`
	Type                  string  `json:"type"`
	ParentID              string  `json:"parent_id,omitempty"`
	Subtitle              string  `json:"subtitle,omitempty"`
	SelectAllChildrenText string  `json:"select_all_children_text,omitempty"`
	Children              []Place `json:"children"`
}

type PlacesRequest struct {
	Method  string            `json:"method"`
	URL     string            `json:"url"`
	Headers map[string]string `json:"headers"`
}

type PlacesCatalog struct {
	GeneratedAt    time.Time     `json:"generated_at"`
	SourcePage     string        `json:"source_page"`
	Request        PlacesRequest `json:"request"`
	Version        string        `json:"version"`
	TotalProvinces int           `json:"total_provinces"`
	TotalCities    int           `json:"total_cities"`
	Root           Place         `json:"root"`
}

// NewPlacesRequest sends a bare POST: no custom headers, cookies, or body.
// Removing curl's --data flag requires retaining POST explicitly; GET returns 403.
func NewPlacesRequest(ctx context.Context) (*http.Request, error) {
	return http.NewRequestWithContext(ctx, http.MethodPost, PlacesURL, nil)
}

// GetPlaces fetches the complete city catalog without writing a file.
func GetPlaces(ctx context.Context) (*PlacesCatalog, error) {
	req, err := NewPlacesRequest(ctx)
	if err != nil {
		return nil, err
	}
	return getPlaces(&http.Client{Timeout: 30 * time.Second}, req)
}

func getPlaces(client *http.Client, req *http.Request) (*PlacesCatalog, error) {
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch places: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetch places: unexpected HTTP status %d", resp.StatusCode)
	}
	const maxResponseBytes = 10 << 20
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read places: %w", err)
	}
	if len(body) > maxResponseBytes {
		return nil, fmt.Errorf("places response exceeds %d bytes", maxResponseBytes)
	}
	var data struct {
		Root    Place  `json:"root"`
		Version string `json:"version"`
	}
	if err := json.Unmarshal(body, &data); err != nil {
		return nil, fmt.Errorf("decode places: %w", err)
	}
	if data.Root.Type != "COUNTRY" || len(data.Root.Children) == 0 {
		return nil, fmt.Errorf("places response must contain a country and its provinces")
	}
	provinces, cities, err := preparePlace(&data.Root, "", "COUNTRY", make(map[string]bool))
	if err != nil {
		return nil, err
	}
	if cities == 0 {
		return nil, fmt.Errorf("places response contains no cities")
	}
	return &PlacesCatalog{
		GeneratedAt:    time.Now().UTC(),
		SourcePage:     PlacesSourcePage,
		Request:        PlacesRequest{Method: req.Method, URL: req.URL.String(), Headers: map[string]string{}},
		Version:        data.Version,
		TotalProvinces: provinces,
		TotalCities:    cities,
		Root:           data.Root,
	}, nil
}

func preparePlace(place *Place, parent, expectedType string, seen map[string]bool) (provinces, cities int, err error) {
	if strings.TrimSpace(place.ID) == "" || strings.TrimSpace(place.Name) == "" {
		return 0, 0, fmt.Errorf("place under %q is missing an ID or name", parent)
	}
	if seen[place.ID] {
		return 0, 0, fmt.Errorf("duplicate place ID %q", place.ID)
	}
	if place.Type != expectedType {
		return 0, 0, fmt.Errorf("place %q has type %q; expected %q", place.ID, place.Type, expectedType)
	}
	seen[place.ID] = true
	place.ParentID = parent
	if place.Children == nil {
		place.Children = []Place{}
	}
	childType := "PROVINCE"
	switch place.Type {
	case "PROVINCE":
		provinces = 1
		childType = "CITY"
		if len(place.Children) == 0 {
			return 0, 0, fmt.Errorf("province %q contains no cities", place.ID)
		}
	case "CITY":
		cities = 1
		if len(place.Children) != 0 {
			return 0, 0, fmt.Errorf("city %q unexpectedly contains children", place.ID)
		}
	}
	for i := range place.Children {
		p, c, err := preparePlace(&place.Children[i], place.ID, childType, seen)
		if err != nil {
			return 0, 0, err
		}
		provinces += p
		cities += c
	}
	return provinces, cities, nil
}

// UpdatePlaces refreshes the dated JSON atomically; failed refreshes preserve
// the previous file. An empty path uses DefaultPlacesFile.
func UpdatePlaces(ctx context.Context, path string) (*PlacesCatalog, error) {
	catalog, err := GetPlaces(ctx)
	if err != nil {
		return nil, err
	}
	if path == "" {
		path = DefaultPlacesFile
	}
	if err := saveCatalogJSON(path, catalog); err != nil {
		return nil, err
	}
	return catalog, nil
}
