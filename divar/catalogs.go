package divar

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"strings"
	"unicode"
)

//go:embed categories.json
var embeddedCategories []byte

//go:embed places.json
var embeddedPlaces []byte

type Catalogs struct {
	Categories map[string]Category
	Cities     map[string]CityReference
}

type CityReference struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Province string `json:"province"`
}

func LoadCatalogs() (*Catalogs, error) {
	var categories CategoryCatalog
	var places PlacesCatalog
	if err := json.Unmarshal(embeddedCategories, &categories); err != nil {
		return nil, err
	}
	if err := json.Unmarshal(embeddedPlaces, &places); err != nil {
		return nil, err
	}
	c := &Catalogs{Categories: map[string]Category{}, Cities: map[string]CityReference{}}
	var walkCats func(Category)
	walkCats = func(n Category) {
		c.Categories[n.Slug] = n
		for _, child := range n.Children {
			walkCats(child)
		}
	}
	walkCats(categories.Root)
	var walkPlaces func(Place, string)
	walkPlaces = func(n Place, province string) {
		if n.Type == "PROVINCE" {
			province = n.Name
		}
		if n.Type == "CITY" {
			c.Cities[n.ID] = CityReference{n.ID, n.Name, province}
		}
		for _, child := range n.Children {
			walkPlaces(child, province)
		}
	}
	walkPlaces(places.Root, "")
	if len(c.Categories) < 2 || len(c.Cities) == 0 {
		return nil, fmt.Errorf("embedded catalogs are empty")
	}
	return c, nil
}

// NormalizePersian makes Persian/Arabic letters and numerals comparable. It is
// for matching and model input; original user wording is retained separately.
func NormalizePersian(text string) string {
	var out strings.Builder
	for _, r := range strings.ToLower(text) {
		switch {
		case r >= '۰' && r <= '۹':
			r = '0' + r - '۰'
		case r >= '٠' && r <= '٩':
			r = '0' + r - '٠'
		}
		switch r {
		case 'ي', 'ى':
			r = 'ی'
		case 'ك':
			r = 'ک'
		case '\u200c', '\u200f', '\u200e':
			r = ' '
		case '٫':
			r = '.'
		case '٬':
			r = ','
		}
		if unicode.Is(unicode.Mn, r) {
			continue
		}
		out.WriteRune(r)
	}
	return strings.Join(strings.Fields(out.String()), " ")
}
