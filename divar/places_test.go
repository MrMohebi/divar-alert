package divar

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const placesFixture = `{"version":"82:p4","root":{"id":"715","name":"ایران","type":"COUNTRY","select_all_children_text":"کل ایران","children":[{"id":"877","name":"کرمان","type":"PROVINCE","children":[{"id":"13","name":"کرمان","type":"CITY","subtitle":"مرکز استان"},{"id":"762","name":"رفسنجان","type":"CITY"}]}]}}`

func TestPlacesBareRequestAndCatalog(t *testing.T) {
	t.Parallel()
	req, err := NewPlacesRequest(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if req.Method != http.MethodPost || req.URL.String() != PlacesURL || len(req.Header) != 0 || req.Body != nil || req.ContentLength != 0 {
		t.Fatalf("expected a bare POST request, got %+v", req)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if r.Method != http.MethodPost || len(body) != 0 {
			t.Errorf("method = %s, body = %q", r.Method, body)
		}
		for _, header := range []string{"Authorization", "Cookie", "Content-Type", "Accept", "Baggage", "Sentry-Trace", "Traceparent"} {
			if r.Header.Get(header) != "" {
				t.Errorf("unexpected %s header", header)
			}
		}
		_, _ = w.Write([]byte(placesFixture))
	}))
	defer server.Close()
	req.URL, _ = url.Parse(server.URL)
	before := time.Now().UTC()
	catalog, err := getPlaces(server.Client(), req)
	if err != nil {
		t.Fatal(err)
	}
	if catalog.TotalCities != 2 || catalog.TotalProvinces != 1 || catalog.Version != "82:p4" || catalog.SourcePage != PlacesSourcePage {
		t.Fatalf("unexpected catalog: %+v", catalog)
	}
	if catalog.GeneratedAt.Before(before) || catalog.GeneratedAt.After(time.Now()) || catalog.GeneratedAt.Location() != time.UTC {
		t.Fatalf("invalid generated_at: %s", catalog.GeneratedAt)
	}
	city := catalog.Root.Children[0].Children[0]
	if city.ID != "13" || city.Name != "کرمان" || city.ParentID != "877" || city.Subtitle != "مرکز استان" || city.Children == nil || len(city.Children) != 0 {
		t.Fatalf("city metadata lost: %+v", city)
	}
	if catalog.Root.SelectAllChildrenText != "کل ایران" || catalog.Root.Children[0].ParentID != "715" || catalog.Root.Children[0].Children[1].Name != "رفسنجان" {
		t.Fatal("hierarchy, ordering, or labels lost")
	}
	if catalog.Request.Method != http.MethodPost || catalog.Request.URL != server.URL || len(catalog.Request.Headers) != 0 {
		t.Fatalf("unexpected request metadata: %+v", catalog.Request)
	}
	path := filepath.Join(t.TempDir(), "nested", "places.json")
	if err := saveCatalogJSON(path, catalog); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var saved PlacesCatalog
	if err := json.Unmarshal(data, &saved); err != nil {
		t.Fatal(err)
	}
	want, _ := json.Marshal(catalog)
	got, _ := json.Marshal(saved)
	if string(want) != string(got) {
		t.Fatal("saved catalog did not round-trip")
	}
}

func TestPlacesRejectsInvalidResponses(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		status int
		body   string
	}{
		{"HTTP error", http.StatusForbidden, "RBAC: access denied"},
		{"missing root", http.StatusOK, `{}`},
		{"truncated JSON", http.StatusOK, placesFixture[:len(placesFixture)-1]},
		{"trailing data", http.StatusOK, placesFixture + "invalid"},
		{"empty country", http.StatusOK, `{"root":{"id":"715","name":"ایران","type":"COUNTRY"}}`},
		{"missing name", http.StatusOK, strings.Replace(placesFixture, `"name":"کرمان"`, `"name":""`, 1)},
		{"missing ID", http.StatusOK, strings.Replace(placesFixture, `"id":"13"`, `"id":""`, 1)},
		{"duplicate ID", http.StatusOK, strings.Replace(placesFixture, `"id":"13"`, `"id":"877"`, 1)},
		{"wrong hierarchy", http.StatusOK, strings.Replace(placesFixture, `"type":"PROVINCE"`, `"type":"CITY"`, 1)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer server.Close()
			req, _ := NewPlacesRequest(context.Background())
			req.URL, _ = url.Parse(server.URL)
			if _, err := getPlaces(server.Client(), req); err == nil {
				t.Fatal("expected invalid response to fail")
			}
		})
	}
}

func TestUpdatePlacesCanceledPreservesFile(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "places.json")
	original := `{"previous":"catalog"}`
	if err := os.WriteFile(path, []byte(original), 0644); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := UpdatePlaces(ctx, path); err == nil {
		t.Fatal("expected cancellation error")
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != original {
		t.Fatalf("previous file changed: %q, error %v", data, err)
	}
}
