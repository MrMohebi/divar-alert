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

// SearchQuery contains only data; endpoints and headers are application-owned.
type SearchQuery struct {
	CityIDs        []string         `json:"city_ids"`
	SearchData     FilterSearchData `json:"search_data"`
	PaginationData json.RawMessage  `json:"pagination_data,omitempty"`
}

type Listing struct {
	Token                 string    `json:"token"`
	Title                 string    `json:"title"`
	ImageURL              string    `json:"image_url,omitempty"`
	TopDescriptionText    string    `json:"top_description_text,omitempty"`
	MiddleDescriptionText string    `json:"middle_description_text,omitempty"`
	BottomDescriptionText string    `json:"bottom_description_text,omitempty"`
	SortDate              time.Time `json:"sort_date"`
}

type SearchPage struct {
	Listings    []Listing
	HasNextPage bool
	// Pagination is opaque: resend every field, including future API additions.
	Pagination json.RawMessage
}

func NewSearchRequest(ctx context.Context, query SearchQuery) (*http.Request, error) {
	if len(query.CityIDs) == 0 || len(query.SearchData.FormData.Data["category"]) == 0 {
		return nil, fmt.Errorf("search requires cities and a category")
	}
	locations := make([]FilterPlace, 0, len(query.CityIDs))
	for _, id := range query.CityIDs {
		if strings.TrimSpace(id) == "" {
			return nil, fmt.Errorf("empty search city ID")
		}
		locations = append(locations, FilterPlace{PlaceID: id})
	}
	// Date order is essential, but other opaque search state must survive.
	payload := map[string]json.RawMessage{}
	if len(query.SearchData.ServerPayload) > 0 {
		if err := json.Unmarshal(query.SearchData.ServerPayload, &payload); err != nil {
			return nil, fmt.Errorf("invalid search state: %w", err)
		}
	}
	if payload == nil {
		payload = map[string]json.RawMessage{}
	}
	if len(payload["@type"]) == 0 {
		payload["@type"] = json.RawMessage(`"type.googleapis.com/widgets.SearchData.ServerPayload"`)
	}
	additional := map[string]json.RawMessage{}
	if len(payload["additional_form_data"]) > 0 {
		if err := json.Unmarshal(payload["additional_form_data"], &additional); err != nil {
			return nil, err
		}
	}
	if additional == nil {
		additional = map[string]json.RawMessage{}
	}
	data := FilterValues{}
	if len(additional["data"]) > 0 {
		if err := json.Unmarshal(additional["data"], &data); err != nil {
			return nil, err
		}
	}
	if data == nil {
		data = FilterValues{}
	}
	data["sort"] = json.RawMessage(`{"str":{"value":"sort_date"}}`)
	additional["data"], _ = json.Marshal(data)
	payload["additional_form_data"], _ = json.Marshal(additional)
	query.SearchData.ServerPayload, _ = json.Marshal(payload)
	query.SearchData.FormData.Data = cloneFormData(query.SearchData.FormData.Data)
	if _, ok := query.SearchData.FormData.Data["sort"]; ok {
		query.SearchData.FormData.Data["sort"] = data["sort"]
	}

	body := struct {
		SearchQuery
		SourceView            string `json:"source_view"`
		DisableRecommendation bool   `json:"disable_recommendation"`
		UserSelectedLocation  struct {
			Places []FilterPlace `json:"places"`
		} `json:"user_selected_location"`
	}{SearchQuery: query, SourceView: "CATEGORY", DisableRecommendation: true}
	body.UserSelectedLocation.Places = locations
	encoded, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("encode search: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.divar.ir"+APIPaths.SearchList, bytes.NewReader(encoded))
	if err == nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return req, err
}

func SearchWithQuery(ctx context.Context, query SearchQuery) (SearchPage, error) {
	req, err := NewSearchRequest(ctx, query)
	if err != nil {
		return SearchPage{}, err
	}
	return searchWithQuery(&http.Client{Timeout: 30 * time.Second}, req)
}

func searchWithQuery(client *http.Client, req *http.Request) (SearchPage, error) {
	body, err := readAPIJSON(client, req)
	if err != nil {
		return SearchPage{}, err
	}
	var raw struct {
		ListWidgets []struct {
			WidgetType string `json:"widget_type"`
			Data       struct {
				Listing
				Action struct {
					Payload struct {
						Token string `json:"token"`
					} `json:"payload"`
				} `json:"action"`
			} `json:"data"`
			ActionLog struct {
				ServerSideInfo struct {
					Info struct {
						SortDate time.Time `json:"sort_date"`
					} `json:"info"`
				} `json:"server_side_info"`
			} `json:"action_log"`
		} `json:"list_widgets"`
		Pagination *struct {
			HasNextPage bool            `json:"has_next_page"`
			Data        json.RawMessage `json:"data"`
		} `json:"pagination"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return SearchPage{}, fmt.Errorf("decode search: %w", err)
	}
	if raw.ListWidgets == nil && raw.Pagination == nil {
		return SearchPage{}, fmt.Errorf("search response missing list_widgets")
	}
	page := SearchPage{Listings: []Listing{}}
	if raw.Pagination != nil {
		page.HasNextPage = raw.Pagination.HasNextPage
		page.Pagination = raw.Pagination.Data
	}
	for _, w := range raw.ListWidgets {
		post := w.Data.Listing
		if post.Token == "" {
			post.Token = w.Data.Action.Payload.Token
		}
		if post.Token == "" {
			continue
		}
		post.SortDate = w.ActionLog.ServerSideInfo.Info.SortDate
		page.Listings = append(page.Listings, post)
	}
	return page, nil
}

func readAPIJSON(client *http.Client, req *http.Request) ([]byte, error) {
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("Divar HTTP status %d", resp.StatusCode)
	}
	const limit = 10 << 20
	body, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, err
	}
	if len(body) > limit {
		return nil, fmt.Errorf("Divar response exceeds size limit")
	}
	return body, nil
}
