package divar

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func testSearchQuery() SearchQuery {
	return SearchQuery{CityIDs: []string{"13"}, SearchData: FilterSearchData{FormData: FilterFormData{Data: FilterValues{"category": json.RawMessage(`{"str":{"value":"light"}}`)}}, Query: "پژو", ServerPayload: json.RawMessage(`{"opaque":"keep","additional_form_data":{"data":{"other":{"str":{"value":"keep"}}}}}`)}}
}

func TestStructuredSearchPreservesOpaqueStateAndPagination(t *testing.T) {
	query := testSearchQuery()
	query.PaginationData = json.RawMessage(`{"page":2,"viewed_tokens":"opaque","future":{"value":42}}`)
	req, err := NewSearchRequest(context.Background(), query)
	if err != nil {
		t.Fatal(err)
	}
	var body map[string]json.RawMessage
	if err = json.NewDecoder(req.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if req.Method != "POST" || len(req.Header) != 1 || req.URL.Host != "api.divar.ir" {
		t.Fatal("unsafe or incorrect request")
	}
	if string(body["pagination_data"]) != string(query.PaginationData) || string(body["disable_recommendation"]) != "true" {
		t.Fatal("lost opaque cursor")
	}
	if !strings.Contains(string(body["search_data"]), `"sort_date"`) || !strings.Contains(string(body["search_data"]), `"opaque":"keep"`) || !strings.Contains(string(body["search_data"]), `"other"`) {
		t.Fatal("lost search state")
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"list_widgets":[{"widget_type":"TITLE_ROW","data":{"title":"not a post"}},{"widget_type":"POST_ROW","data":{"title":"خودرو","token":"abc"},"action_log":{"server_side_info":{"info":{"sort_date":"2026-10-20T10:00:00Z"}}}}],"pagination":{"has_next_page":true,"data":{"page":3,"future":"preserved"}}}`)
	}))
	defer server.Close()
	req, _ = NewSearchRequest(context.Background(), query)
	req.URL, _ = url.Parse(server.URL)
	page, err := searchWithQuery(server.Client(), req)
	if err != nil || len(page.Listings) != 1 || page.Listings[0].SortDate.IsZero() || !page.HasNextPage || !strings.Contains(string(page.Pagination), "preserved") {
		t.Fatalf("page=%+v, err=%v", page, err)
	}
}

func TestLazyHierarchyAndCatalogs(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"options":{"data":{"key":"ROOT"},"children":[{"data":{"key":"Peugeot","title":"پژو"},"children":[{"data":{"key":"Peugeot 206","title":"پژو ۲۰۶"}}]}]}}`)
	}))
	defer server.Close()
	req, _ := http.NewRequest("POST", server.URL, nil)
	options, err := getLazyFilterOptions(server.Client(), req)
	if err != nil || options[0].Children[0].Key != "Peugeot 206" {
		t.Fatalf("options=%+v err=%v", options, err)
	}
	catalogs, err := LoadCatalogs()
	if err != nil {
		t.Fatal(err)
	}
	if catalogs.Cities["13"].Name != "کرمان" || catalogs.Categories["partnership"].Name == "" {
		t.Fatal("missing embedded data")
	}
	if NormalizePersian("كـي ١۲۳") != "کـی 123" {
		t.Fatal("normalization failed")
	}
}

func TestFilterSelectionValidation(t *testing.T) {
	f := FilterDefinition{Key: "price", ValueType: "number_range", Validators: json.RawMessage(`{"minimum_validators":[{"min":{}}],"maximum_validators":[{"max":{"max":"1000"}}]}`)}
	for _, raw := range []string{`{"minimum":"-1"}`, `{"maximum":"1001"}`, `{"minimum":"2","maximum":"1"}`} {
		if _, err := f.DecodeSelection(json.RawMessage(raw)); err == nil {
			t.Fatalf("invalid range accepted %s", raw)
		}
	}
	f = FilterDefinition{Key: "gearbox", ValueType: "str", Options: []FilterOption{{Value: "automatic", Label: "اتوماتیک"}}}
	if _, err := f.DecodeSelection(json.RawMessage(`"invented"`)); err == nil {
		t.Fatal("invented option accepted")
	}
	if _, err := f.DecodeSelection(json.RawMessage(`"automatic"`)); err != nil {
		t.Fatal(err)
	}
}

func TestEmptyLastSearchPageOmitsRepeatedWidgets(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"pagination":{"data":{"page":2}},"show_no_search_result_notice":true}`)
	}))
	defer server.Close()
	req, _ := NewSearchRequest(context.Background(), testSearchQuery())
	req.URL, _ = url.Parse(server.URL)
	page, err := searchWithQuery(server.Client(), req)
	if err != nil || len(page.Listings) != 0 || page.HasNextPage {
		t.Fatalf("empty terminal page rejected: %+v %v", page, err)
	}
}
