package divar

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

const categoryFixture = `{"category":{"name":"همهٔ آگهی‌ها","slug":"ROOT","children":[
  {"name":"املاک","slug":"real-estate","children":[
    {"name":"اجارهٔ مسکونی","slug":"residential-rent","children":[
      {"name":"آپارتمان","second_name":"اجارهٔ آپارتمان","slug":"apartment-rent","icon":{"icon_name":"APARTMENT_RENT"}}
    ]}
  ]},
  {"name":"وسایل نقلیه","slug":"vehicles"}
]}}`

func TestGetCategoriesPreservesEntireTree(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.Header.Get("Accept") != "application/json" {
			t.Errorf("unexpected request: %s %v", r.Method, r.Header)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(categoryFixture))
	}))
	defer server.Close()
	req, err := NewCategoriesRequest(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if req.URL.String() != CategoriesURL {
		t.Fatalf("request URL = %s", req.URL)
	}
	req.URL, _ = url.Parse(server.URL)
	before := time.Now().UTC()
	catalog, err := getCategories(server.Client(), req)
	if err != nil {
		t.Fatal(err)
	}
	if catalog.TotalCategories != 4 || catalog.SourcePage != CategoriesSourcePage {
		t.Fatalf("unexpected catalog: %+v", catalog)
	}
	if catalog.GeneratedAt.Before(before) || catalog.GeneratedAt.After(time.Now()) || catalog.GeneratedAt.Location() != time.UTC {
		t.Fatalf("invalid generation time: %s", catalog.GeneratedAt)
	}
	leaf := catalog.Root.Children[0].Children[0].Children[0]
	if leaf.Name != "آپارتمان" || leaf.SecondName != "اجارهٔ آپارتمان" || leaf.ParentSlug != "residential-rent" || leaf.Children == nil || len(leaf.Children) != 0 {
		t.Fatalf("nested leaf was lost or changed: %+v", leaf)
	}
	if string(leaf.Icon) != `{"icon_name":"APARTMENT_RENT"}` || catalog.Root.Children[1].Slug != "vehicles" || catalog.Root.Children[1].ParentSlug != "ROOT" {
		t.Fatal("icons, ordering, or parent relationships were lost")
	}
	if catalog.Request.Method != http.MethodGet || catalog.Request.URL != server.URL || catalog.Request.Headers["Accept"] != "application/json" {
		t.Fatalf("request metadata = %+v", catalog.Request)
	}
	path := filepath.Join(t.TempDir(), "nested", "categories.json")
	if err := saveCategoryCatalog(path, catalog); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var saved CategoryCatalog
	if err := json.Unmarshal(data, &saved); err != nil {
		t.Fatal(err)
	}
	// RawMessage indentation changes when marshaled; compare canonical JSON.
	want, _ := json.Marshal(catalog)
	got, _ := json.Marshal(saved)
	if string(got) != string(want) {
		t.Fatal("saved catalog did not round-trip")
	}
}

func TestGetCategoriesRejectsBadResponses(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		status int
		body   string
	}{
		{"http error", http.StatusServiceUnavailable, categoryFixture},
		{"invalid JSON", http.StatusOK, `<html>error</html>`},
		{"truncated JSON", http.StatusOK, categoryFixture[:len(categoryFixture)-1]},
		{"trailing garbage", http.StatusOK, categoryFixture + `invalid`},
		{"missing tree", http.StatusOK, `{}`},
		{"empty root", http.StatusOK, `{"category":{"slug":"ROOT","name":"All"}}`},
		{"wrong root", http.StatusOK, `{"category":{"slug":"other","name":"All","children":[{"slug":"leaf","name":"Leaf"}]}}`},
		{"missing nested slug", http.StatusOK, `{"category":{"slug":"ROOT","name":"All","children":[{"name":"Leaf"}]}}`},
		{"duplicate slug", http.StatusOK, `{"category":{"slug":"ROOT","name":"All","children":[{"slug":"same","name":"One"},{"slug":"same","name":"Two"}]}}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer server.Close()
			req, _ := NewCategoriesRequest(context.Background())
			req.URL, _ = url.Parse(server.URL)
			if _, err := getCategories(server.Client(), req); err == nil {
				t.Fatal("expected invalid response to fail")
			}
		})
	}
}

func TestUpdateCategoriesFailurePreservesFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "categories.json")
	original := []byte(`{"previous":"catalog"}`)
	if err := os.WriteFile(path, original, 0644); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := UpdateCategories(ctx, path); err == nil {
		t.Fatal("expected canceled request to fail")
	}
	if err := saveCategoryCatalog(path, &CategoryCatalog{Root: Category{Icon: json.RawMessage(`invalid`)}}); err == nil {
		t.Fatal("expected encoding failure")
	}
	got, err := os.ReadFile(path)
	if err != nil || !reflect.DeepEqual(got, original) {
		t.Fatalf("previous file changed: %q, error %v", got, err)
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Fatalf("temporary files left behind: %v", entries)
	}
}
