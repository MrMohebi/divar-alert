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
	CategoriesURL         = "https://api.divar.ir/v8/postlist/w/categories"
	CategoriesSourcePage  = "https://divar.ir/s/kerman"
	DefaultCategoriesFile = "divar/categories.json"
)

// Category preserves Divar's names, slugs, icons, order, and nested children.
// Slugs are API identifiers; they can differ from the website's URL aliases.
type Category struct {
	Name       string          `json:"name"`
	SecondName string          `json:"second_name,omitempty"`
	Slug       string          `json:"slug"`
	ParentSlug string          `json:"parent_slug,omitempty"`
	Icon       json.RawMessage `json:"icon,omitempty"`
	Children   []Category      `json:"children"`
}

// CategoryRequest records the public request needed to refresh the entire tree.
type CategoryRequest struct {
	Method  string            `json:"method"`
	URL     string            `json:"url"`
	Headers map[string]string `json:"headers"`
}

type CategoryCatalog struct {
	GeneratedAt     time.Time       `json:"generated_at"`
	SourcePage      string          `json:"source_page"`
	Request         CategoryRequest `json:"request"`
	TotalCategories int             `json:"total_categories"` // Excludes the synthetic ROOT.
	Root            Category        `json:"root"`
}

// NewCategoriesRequest builds the same public request used by the category menu.
// The API returns every level in one response and does not require a city or login.
func NewCategoriesRequest(ctx context.Context) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, CategoriesURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	return req, nil
}

// GetCategories fetches and validates the full category tree without writing files.
func GetCategories(ctx context.Context) (*CategoryCatalog, error) {
	req, err := NewCategoriesRequest(ctx)
	if err != nil {
		return nil, err
	}
	return getCategories(&http.Client{Timeout: 30 * time.Second}, req)
}

func getCategories(client *http.Client, req *http.Request) (*CategoryCatalog, error) {
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch categories: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetch categories: unexpected HTTP status %d", resp.StatusCode)
	}
	// Decode a complete JSON document so truncated or trailing invalid data fails.
	const maxResponseBytes = 10 << 20
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read categories: %w", err)
	}
	if len(body) > maxResponseBytes {
		return nil, fmt.Errorf("categories response exceeds %d bytes", maxResponseBytes)
	}
	var data struct {
		Category Category `json:"category"`
	}
	if err := json.Unmarshal(body, &data); err != nil {
		return nil, fmt.Errorf("decode categories: %w", err)
	}
	if data.Category.Slug != "ROOT" || len(data.Category.Children) == 0 {
		return nil, fmt.Errorf("categories response must contain ROOT and its children")
	}
	count, err := prepareCategory(&data.Category, "", make(map[string]bool))
	if err != nil {
		return nil, err
	}
	return &CategoryCatalog{
		GeneratedAt: time.Now().UTC(),
		SourcePage:  CategoriesSourcePage,
		Request: CategoryRequest{
			Method:  req.Method,
			URL:     req.URL.String(),
			Headers: map[string]string{"Accept": req.Header.Get("Accept")},
		},
		TotalCategories: count - 1,
		Root:            data.Category,
	}, nil
}

func prepareCategory(category *Category, parent string, seen map[string]bool) (int, error) {
	if strings.TrimSpace(category.Name) == "" || strings.TrimSpace(category.Slug) == "" {
		return 0, fmt.Errorf("category under %q is missing a name or slug", parent)
	}
	if seen[category.Slug] {
		return 0, fmt.Errorf("duplicate category slug %q", category.Slug)
	}
	seen[category.Slug] = true
	category.ParentSlug = parent
	if category.Children == nil {
		category.Children = []Category{}
	}
	count := 1
	for i := range category.Children {
		n, err := prepareCategory(&category.Children[i], category.Slug, seen)
		if err != nil {
			return 0, err
		}
		count += n
	}
	return count, nil
}

// UpdateCategories refreshes path atomically, preserving the previous file on
// request, validation, or write failure. An empty path uses DefaultCategoriesFile.
func UpdateCategories(ctx context.Context, path string) (*CategoryCatalog, error) {
	catalog, err := GetCategories(ctx)
	if err != nil {
		return nil, err
	}
	if path == "" {
		path = DefaultCategoriesFile
	}
	if err := saveCategoryCatalog(path, catalog); err != nil {
		return nil, err
	}
	return catalog, nil
}

func saveCategoryCatalog(path string, catalog *CategoryCatalog) error {
	return saveCatalogJSON(path, catalog)
}
