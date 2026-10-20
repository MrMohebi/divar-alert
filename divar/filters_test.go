package divar

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"testing"
)

const filtersFixture = `{
 "page":{"title":"فیلترها","server_payload":{"@type":"UI_ONLY"},"widget_list":[
  {"widget_type":"TITLE_ROW","data":{"text":"قیمت","description":"(تومان)","corresponding_input_widget_id":"filter_price"}},
  {"widget_type":"I_SELECTIVE_RANGE_ROW","uid":"filter_price","data":{"field":{"key":"price","type":"number_range","default":{},"validators":{"minimum_validators":[{"min":{}}]}},"from_options":[{"display":"صفر"},{"value":"1000","display":"۱۰۰۰"}],"to_options":[{"value":"5000","display":"۵۰۰۰"}],"minimum_dynamic_suggestion":"100","maximum_dynamic_suggestion":"10000000000000"}},
  {"widget_type":"TITLE_ROW","data":{"text":"وضعیت آگهی"}},
  {"widget_type":"I_TOGGLE_ROW","data":{"title":"عکس‌دار","field":{"key":"has-photo","type":"boolean"}}},
  {"widget_type":"I_MULTI_SELECT_V2_ROW","data":{"filter_page_title":"آگهی‌دهنده","field":{"key":"business-type","type":"repeated_string"},"options":[{"key":"personal","title":"شخصی"}],"select_all_key":"ALL_POSSIBLE_OPTIONS"}},
  {"widget_type":"I_SINGLE_SELECT_ROW","data":{"title":"زمان انتشار آگهی","field":{"key":"recent_ads","type":"str"},"options":[{"value":"1d","display":"۱ روز پیش"}]}},
  {"widget_type":"I_LAZY_MULTI_SELECT_HIERARCHY_ROW","data":{"filter_page_title":"برند و مدل","field":{"key":"brand_model","type":"repeated_string"},"lazy_payload":{"filter_name":"brand_model","category":"light","version":"test-version"},"online_search":{"search_key":"car_brand_model","min_query_length":2}}},
  {"widget_type":"FUTURE_WIDGET","data":{"title":"جدید","field":{"key":"future","type":"future_type"},"future_detail":123}}
 ]},
 "base_data":{"form_data":{"data":{"category":{"str":{"value":"light"}}}}}
}`

func TestFiltersRequestAndCleanResponse(t *testing.T) {
	t.Parallel()
	selected := FilterValues{"recent_ads": json.RawMessage(`{"str":{"value":"1d"}}`)}
	params := FiltersParams{Category: "light", CityIDs: []string{"13", "1"}, FormData: selected}
	req, err := NewFiltersRequest(context.Background(), params)
	if err != nil {
		t.Fatal(err)
	}
	if req.Method != http.MethodPost || req.URL.String() != FiltersURL || len(req.Header) != 1 || req.Header.Get("Content-Type") != "application/json" {
		t.Fatalf("unexpected request: %+v", req)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		for _, header := range []string{"Authorization", "Cookie", "Baggage", "Sentry-Trace", "Traceparent"} {
			if r.Header.Get(header) != "" {
				t.Errorf("unexpected %s header", header)
			}
		}
		var body FiltersRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		if body.SourceView != "CATEGORY" || !reflect.DeepEqual(body.CityIDs, params.CityIDs) || !reflect.DeepEqual(body.UserSelectedLocation.Places, []FilterPlace{{PlaceID: "13"}, {PlaceID: "1"}}) {
			t.Errorf("wrong category/location request: %+v", body)
		}
		if string(body.Data.FormData.Data["category"]) != `{"str":{"value":"light"}}` || !strings.Contains(string(body.Data.ServerPayload), "sort_date") || string(body.Data.FormData.Data["recent_ads"]) != string(selected["recent_ads"]) {
			t.Errorf("lost search state: %+v", body.Data)
		}
		_, _ = io.WriteString(w, filtersFixture)
	}))
	defer server.Close()
	req.URL, _ = url.Parse(server.URL)
	result, err := getFilters(server.Client(), req)
	if err != nil {
		t.Fatal(err)
	}
	if result.Title != "فیلترها" || result.Category != "light" || len(result.Filters) != 6 || !reflect.DeepEqual(result.CityIDs, params.CityIDs) {
		t.Fatalf("unexpected result: %+v", result)
	}
	price := result.Filters[0]
	if price.Title != "قیمت" || price.Unit != "تومان" || price.ValueType != "number_range" || price.FromOptions[0].Value != "0" || price.ToOptions[0].Value != "5000" || price.SuggestedMaximum != "10000000000000" || len(price.Validators) == 0 || len(price.Default) == 0 {
		t.Fatalf("lost range metadata: %+v", price)
	}
	if result.Filters[1].Section != "وضعیت آگهی" || result.Filters[2].Title != "آگهی‌دهنده" || result.Filters[2].Options[0].Value != "personal" || result.Filters[2].SelectAllValue != "ALL_POSSIBLE_OPTIONS" || result.Filters[3].Options[0].Label != "۱ روز پیش" {
		t.Fatal("lost labels, groups, or options")
	}
	if len(result.Filters[4].LazyPayload) == 0 || len(result.Filters[4].OnlineSearch) == 0 || len(result.Filters[4].Options) != 0 || !strings.Contains(string(result.Filters[5].RawData), "future_detail") {
		t.Fatal("lost lazy or unknown field metadata")
	}
	if string(result.Filters[3].Selected) != string(selected["recent_ads"]) || strings.Contains(string(result.SearchData.ServerPayload), "UI_ONLY") {
		t.Fatal("search state was lost or replaced by UI server payload")
	}
	form, err := result.BuildFormData(map[string]any{
		"price":     FilterNumberRange{Minimum: "0", Maximum: "10000000000000"},
		"has-photo": false, "business-type": []string{"personal"}, "recent_ads": nil,
	})
	if err != nil {
		t.Fatal(err)
	}
	want := FilterValues{
		"category":      json.RawMessage(`{"str":{"value":"light"}}`),
		"price":         json.RawMessage(`{"number_range":{"minimum":"0","maximum":"10000000000000"}}`),
		"has-photo":     json.RawMessage(`{"boolean":{"value":false}}`),
		"business-type": json.RawMessage(`{"repeated_string":{"value":["personal"]}}`),
	}
	if !reflect.DeepEqual(form, want) {
		t.Fatalf("form = %s", mustJSON(t, form))
	}
	if _, ok := selected["category"]; ok {
		t.Fatal("request mutated caller's form")
	}
	if _, ok := result.SearchData.FormData.Data["price"]; ok {
		t.Fatal("selection mutated original search state")
	}
	form["category"][0] = 'x'
	if result.SearchData.FormData.Data["category"][0] != '{' {
		t.Fatal("form shares mutable JSON with result")
	}
}

func mustJSON(t *testing.T, value any) string {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestFilterValueEncoding(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		kind  string
		value any
		want  string
	}{
		{"str", "1d", `{"str":{"value":"1d"}}`},
		{"boolean", true, `{"boolean":{"value":true}}`},
		{"repeated_string", []string(nil), `{"repeated_string":{"value":[]}}`},
		{"number_range", FilterNumberRange{Maximum: "0"}, `{"number_range":{"maximum":"0"}}`},
		{"number_range", FilterNumberRange{Minimum: "-2", Maximum: "3"}, `{"number_range":{"minimum":"-2","maximum":"3"}}`},
	} {
		got, err := (FilterDefinition{Key: "test", ValueType: tc.kind}).EncodeValue(tc.value)
		if err != nil || string(got) != tc.want {
			t.Fatalf("%s: got %s, error %v", tc.kind, got, err)
		}
	}
	for _, tc := range []struct {
		kind  string
		value any
	}{
		{"str", 12}, {"boolean", "false"}, {"repeated_string", []int{1}},
		{"number_range", map[string]string{"minimum": "1"}},
		{"number_range", FilterNumberRange{}},
		{"number_range", FilterNumberRange{Minimum: "1.5"}},
		{"number_range", FilterNumberRange{Minimum: "9", Maximum: "2"}},
		{"future_type", "value"},
	} {
		if _, err := (FilterDefinition{Key: "test", ValueType: tc.kind}).EncodeValue(tc.value); err == nil {
			t.Fatalf("accepted invalid %s value %v", tc.kind, tc.value)
		}
	}
	if _, err := (FiltersResult{}).BuildFormData(map[string]any{"unknown": true}); err == nil {
		t.Fatal("unknown filter accepted")
	}
}

func TestFiltersErrors(t *testing.T) {
	t.Parallel()
	for _, params := range []FiltersParams{
		{}, {Category: "light"}, {Category: "light", CityIDs: []string{""}},
		{Category: "light", CityIDs: []string{"13"}, FormData: FilterValues{"x": json.RawMessage(`invalid`)}},
	} {
		if _, err := NewFiltersRequest(context.Background(), params); err == nil {
			t.Fatalf("accepted invalid params %+v", params)
		}
	}
	for _, tc := range []struct {
		name   string
		status int
		body   string
	}{
		{"HTTP error", http.StatusForbidden, `denied`},
		{"invalid JSON", http.StatusOK, `<html>error</html>`},
		{"missing page", http.StatusOK, `{}`},
		{"missing widgets", http.StatusOK, `{"page":{}}`},
		{"trailing data", http.StatusOK, filtersFixture + `invalid`},
		{"duplicate key", http.StatusOK, strings.Replace(filtersFixture, `"key":"future"`, `"key":"price"`, 1)},
		{"missing type", http.StatusOK, strings.Replace(filtersFixture, `"type":"future_type"`, `"type":""`, 1)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = io.WriteString(w, tc.body)
			}))
			defer server.Close()
			req, _ := NewFiltersRequest(context.Background(), FiltersParams{Category: "light", CityIDs: []string{"13"}})
			req.URL, _ = url.Parse(server.URL)
			if _, err := getFilters(server.Client(), req); err == nil {
				t.Fatal("invalid response accepted")
			}
		})
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := GetFilters(ctx, FiltersParams{Category: "light", CityIDs: []string{"13"}}); err == nil {
		t.Fatal("canceled request succeeded")
	}
}
