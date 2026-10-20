package divar

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const FiltersURL = "https://api.divar.ir/v8/postlist/w/filters"

// FilterValues maps exact Divar field keys to their protobuf JSON values.
// Example: {"has-photo":{"boolean":{"value":true}}}.
type FilterValues map[string]json.RawMessage

type FilterFormData struct {
	Data FilterValues `json:"data"`
}

// FilterSearchData can be reused as data in a search request. ServerPayload is
// search state, distinct from page.server_payload used by the filters UI.
type FilterSearchData struct {
	FormData      FilterFormData  `json:"form_data"`
	ServerPayload json.RawMessage `json:"server_payload,omitempty"`
	Query         string          `json:"query,omitempty"`
}

type FiltersParams struct {
	Category string
	CityIDs  []string
	// FormData contains already encoded selections when refreshing dependent filters.
	FormData      FilterValues
	ServerPayload json.RawMessage
}

// FiltersRequest is the dynamic filters endpoint's request body.
type FiltersRequest struct {
	Data                 FilterSearchData `json:"data"`
	SourceView           string           `json:"source_view"`
	CityIDs              []string         `json:"city_ids"`
	UserSelectedLocation struct {
		Places []FilterPlace `json:"places"`
	} `json:"user_selected_location"`
}

type FilterPlace struct {
	PlaceID string `json:"place_id"`
}

// FiltersResponse preserves the response structure independently of the cleaned
// return format, so normalization can change without changing the HTTP request.
type FiltersResponse struct {
	Page     *FiltersPage     `json:"page"`
	BaseData FilterSearchData `json:"base_data"`
}

type FiltersPage struct {
	Title         string          `json:"title"`
	Widgets       []FilterWidget  `json:"widget_list"`
	ServerPayload json.RawMessage `json:"server_payload,omitempty"`
}

type FilterWidget struct {
	WidgetType           string          `json:"widget_type"`
	UID                  string          `json:"uid,omitempty"`
	Data                 json.RawMessage `json:"data"`
	VisibilityConditions json.RawMessage `json:"visibility_conditions,omitempty"`
}

type FiltersResult struct {
	Category   string             `json:"category"`
	CityIDs    []string           `json:"city_ids"`
	Title      string             `json:"title"`
	Filters    []FilterDefinition `json:"filters"`
	SearchData FilterSearchData   `json:"search_data"`
}

// FilterDefinition keeps search-relevant metadata and discards display styling
// and analytics. RawData remains available in memory for future normalization.
type FilterDefinition struct {
	Key                  string          `json:"key"`
	ValueType            string          `json:"value_type"`
	WidgetType           string          `json:"widget_type"`
	Title                string          `json:"title"`
	Section              string          `json:"section,omitempty"`
	Unit                 string          `json:"unit,omitempty"`
	Options              []FilterOption  `json:"options,omitempty"`
	FromOptions          []FilterOption  `json:"from_options,omitempty"`
	ToOptions            []FilterOption  `json:"to_options,omitempty"`
	Default              json.RawMessage `json:"default,omitempty"`
	Selected             json.RawMessage `json:"selected,omitempty"`
	Validators           json.RawMessage `json:"validators,omitempty"`
	SuggestedMinimum     string          `json:"suggested_minimum,omitempty"`
	SuggestedMaximum     string          `json:"suggested_maximum,omitempty"`
	SelectAllValue       string          `json:"select_all_value,omitempty"`
	LazyPayload          json.RawMessage `json:"lazy_payload,omitempty"`
	OnlineSearch         json.RawMessage `json:"online_search,omitempty"`
	VisibilityConditions json.RawMessage `json:"visibility_conditions,omitempty"`
	RawData              json.RawMessage `json:"-"`
}

type FilterOption struct {
	Value    string         `json:"value"`
	Label    string         `json:"label"`
	Children []FilterOption `json:"children,omitempty"`
}

type filterWidgetData struct {
	Title                      string `json:"title"`
	Text                       string `json:"text"`
	Description                string `json:"description"`
	FilterPageTitle            string `json:"filter_page_title"`
	CorrespondingInputWidgetID string `json:"corresponding_input_widget_id"`
	Unit                       string `json:"unit"`
	Field                      struct {
		Key        string          `json:"key"`
		Type       string          `json:"type"`
		Default    json.RawMessage `json:"default"`
		Validators json.RawMessage `json:"validators"`
	} `json:"field"`
	Options                  []filterOptionData `json:"options"`
	FromOptions              []filterOptionData `json:"from_options"`
	ToOptions                []filterOptionData `json:"to_options"`
	MinimumDynamicSuggestion string             `json:"minimum_dynamic_suggestion"`
	MaximumDynamicSuggestion string             `json:"maximum_dynamic_suggestion"`
	SelectAllKey             string             `json:"select_all_key"`
	LazyPayload              json.RawMessage    `json:"lazy_payload"`
	OnlineSearch             json.RawMessage    `json:"online_search"`
}

type filterOptionData struct {
	Value    string             `json:"value"`
	Key      string             `json:"key"`
	Display  string             `json:"display"`
	Title    string             `json:"title"`
	Children []filterOptionData `json:"children"`
}

// NewFiltersRequest builds a POST with only Content-Type and dynamic search data.
// Category must be an API slug (or ROOT), and CityIDs come from GetPlaces.
func NewFiltersRequest(ctx context.Context, params FiltersParams) (*http.Request, error) {
	if strings.TrimSpace(params.Category) == "" || len(params.CityIDs) == 0 {
		return nil, fmt.Errorf("category and at least one city ID are required")
	}
	data := cloneFormData(params.FormData)
	category, _ := json.Marshal(map[string]any{"str": map[string]string{"value": params.Category}})
	data["category"] = category
	payload := params.ServerPayload
	if len(payload) == 0 {
		payload = json.RawMessage(`{"@type":"type.googleapis.com/widgets.SearchData.ServerPayload","additional_form_data":{"data":{"sort":{"str":{"value":"sort_date"}}}}}`)
	}
	body := FiltersRequest{
		Data:       FilterSearchData{FormData: FilterFormData{Data: data}, ServerPayload: payload},
		SourceView: "CATEGORY",
		CityIDs:    append([]string(nil), params.CityIDs...),
	}
	for _, id := range params.CityIDs {
		if strings.TrimSpace(id) == "" {
			return nil, fmt.Errorf("city IDs must not be blank")
		}
		body.UserSelectedLocation.Places = append(body.UserSelectedLocation.Places, FilterPlace{PlaceID: id})
	}
	encoded, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("encode filters request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, FiltersURL, bytes.NewReader(encoded))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	return req, nil
}

// GetFilters fetches current filters for the selected category/cities/selections.
// It does not cache responses or write files.
func GetFilters(ctx context.Context, params FiltersParams) (*FiltersResult, error) {
	req, err := NewFiltersRequest(ctx, params)
	if err != nil {
		return nil, err
	}
	return getFilters(&http.Client{Timeout: 30 * time.Second}, req)
}

func getFilters(client *http.Client, req *http.Request) (*FiltersResult, error) {
	// Retain the actual request's form state; base_data can omit existing selections.
	requestBody, err := req.GetBody()
	if err != nil {
		return nil, err
	}
	defer requestBody.Close()
	var input FiltersRequest
	if err := json.NewDecoder(requestBody).Decode(&input); err != nil {
		return nil, fmt.Errorf("decode filters request state: %w", err)
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch filters: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetch filters: unexpected HTTP status %d", resp.StatusCode)
	}
	const maxResponseBytes = 10 << 20
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read filters: %w", err)
	}
	if len(body) > maxResponseBytes {
		return nil, fmt.Errorf("filters response exceeds %d bytes", maxResponseBytes)
	}
	var response FiltersResponse
	if err := json.Unmarshal(body, &response); err != nil {
		return nil, fmt.Errorf("decode filters: %w", err)
	}
	return CleanFilters(response, input)
}

// CleanFilters is the single conversion point for the public return structure.
func CleanFilters(response FiltersResponse, input FiltersRequest) (*FiltersResult, error) {
	if response.Page == nil || response.Page.Widgets == nil {
		return nil, fmt.Errorf("filters response is missing page.widget_list")
	}
	result := &FiltersResult{
		Title: response.Page.Title, CityIDs: append([]string(nil), input.CityIDs...),
		Filters: []FilterDefinition{}, SearchData: input.Data,
	}
	result.SearchData.FormData.Data = cloneFormData(input.Data.FormData.Data)
	for key, value := range response.BaseData.FormData.Data {
		result.SearchData.FormData.Data[key] = append(json.RawMessage(nil), value...)
	}
	if len(response.BaseData.ServerPayload) != 0 {
		result.SearchData.ServerPayload = response.BaseData.ServerPayload
	}
	var category struct {
		Str struct {
			Value string `json:"value"`
		} `json:"str"`
	}
	if err := json.Unmarshal(result.SearchData.FormData.Data["category"], &category); err != nil {
		return nil, fmt.Errorf("filters category: %w", err)
	}
	result.Category = category.Str.Value
	// Range labels live in separate TITLE_ROW widgets linked by UID.
	labels := make(map[string]filterWidgetData)
	decoded := make([]filterWidgetData, len(response.Page.Widgets))
	for i, widget := range response.Page.Widgets {
		if err := json.Unmarshal(widget.Data, &decoded[i]); err != nil {
			return nil, fmt.Errorf("decode filter widget %q: %w", widget.WidgetType, err)
		}
		if id := decoded[i].CorrespondingInputWidgetID; id != "" {
			labels[id] = decoded[i]
		}
	}
	section := ""
	seen := make(map[string]bool)
	for i, widget := range response.Page.Widgets {
		d := decoded[i]
		if widget.WidgetType == "TITLE_ROW" && d.CorrespondingInputWidgetID == "" {
			section = d.Text
		}
		if d.Field.Key == "" {
			continue
		}
		if d.Field.Type == "" || seen[d.Field.Key] {
			return nil, fmt.Errorf("filter %q has a missing type or duplicate key", d.Field.Key)
		}
		seen[d.Field.Key] = true
		title := firstNonempty(d.Title, d.FilterPageTitle, labels[widget.UID].Text, d.Field.Key)
		unit := firstNonempty(d.Unit, strings.Trim(labels[widget.UID].Description, "() "))
		result.Filters = append(result.Filters, FilterDefinition{
			Key: d.Field.Key, ValueType: d.Field.Type, WidgetType: widget.WidgetType,
			Title: title, Section: section, Unit: unit,
			Options:     cleanFilterOptions(d.Options, false),
			FromOptions: cleanFilterOptions(d.FromOptions, true),
			ToOptions:   cleanFilterOptions(d.ToOptions, true),
			Default:     d.Field.Default, Selected: result.SearchData.FormData.Data[d.Field.Key],
			Validators:       d.Field.Validators,
			SuggestedMinimum: d.MinimumDynamicSuggestion, SuggestedMaximum: d.MaximumDynamicSuggestion,
			SelectAllValue: d.SelectAllKey, LazyPayload: d.LazyPayload, OnlineSearch: d.OnlineSearch,
			VisibilityConditions: widget.VisibilityConditions, RawData: widget.Data,
		})
	}
	return result, nil
}

func cleanFilterOptions(options []filterOptionData, numeric bool) []FilterOption {
	var result []FilterOption
	for _, option := range options {
		value := firstNonempty(option.Value, option.Key)
		// Protobuf omits the default numeric value (zero) from range suggestions.
		if value == "" && numeric {
			value = "0"
		}
		result = append(result, FilterOption{Value: value, Label: firstNonempty(option.Display, option.Title, value), Children: cleanFilterOptions(option.Children, numeric)})
	}
	return result
}

func firstNonempty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func cloneFormData(data FilterValues) FilterValues {
	copy := make(FilterValues, len(data))
	for key, value := range data {
		copy[key] = append(json.RawMessage(nil), value...)
	}
	return copy
}
