package divar

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

const LazyFilterOptionsURL = "https://api.divar.ir/v8/w/lazy-multi-select-hierarchy-options"

type LazyFilterOption struct {
	Key            string             `json:"key"`
	Label          string             `json:"label"`
	SearchKeywords string             `json:"search_keywords,omitempty"`
	Children       []LazyFilterOption `json:"children,omitempty"`
}

func GetLazyFilterOptions(ctx context.Context, payload json.RawMessage) ([]LazyFilterOption, error) {
	if len(payload) == 0 || !json.Valid(payload) {
		return nil, fmt.Errorf("invalid lazy filter payload")
	}
	body, err := json.Marshal(struct {
		Payload json.RawMessage `json:"payload"`
	}{payload})
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, LazyFilterOptionsURL, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	return getLazyFilterOptions(&http.Client{Timeout: 30 * time.Second}, req)
}

func getLazyFilterOptions(client *http.Client, req *http.Request) ([]LazyFilterOption, error) {
	body, err := readAPIJSON(client, req)
	if err != nil {
		return nil, err
	}
	type node struct {
		Data struct {
			Key            string `json:"key"`
			Title          string `json:"title"`
			SearchKeywords string `json:"search_keywords"`
		} `json:"data"`
		Children []json.RawMessage `json:"children"`
	}
	var raw struct {
		Options json.RawMessage `json:"options"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, err
	}
	var convert func(json.RawMessage) (LazyFilterOption, error)
	convert = func(b json.RawMessage) (LazyFilterOption, error) {
		var n node
		if err := json.Unmarshal(b, &n); err != nil {
			return LazyFilterOption{}, err
		}
		if n.Data.Key == "" {
			return LazyFilterOption{}, fmt.Errorf("lazy option missing key")
		}
		o := LazyFilterOption{Key: n.Data.Key, Label: n.Data.Title, SearchKeywords: n.Data.SearchKeywords}
		for _, child := range n.Children {
			c, err := convert(child)
			if err != nil {
				return o, err
			}
			o.Children = append(o.Children, c)
		}
		return o, nil
	}
	root, err := convert(raw.Options)
	if err != nil {
		return nil, err
	}
	return root.Children, nil
}
