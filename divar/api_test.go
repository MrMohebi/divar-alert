package divar

import "testing"

func TestParseCurlCommand(t *testing.T) {
	t.Parallel()

	command := `curl 'https://api.divar.ir/v8/postlist/w/search' -H 'content-type: application/json' --data-raw '{"query":"two-bedroom"}'`
	rawURL, headers, body, err := parseCurlCommand(command)
	if err != nil {
		t.Fatalf("parseCurlCommand() error = %v", err)
	}
	if rawURL != "https://api.divar.ir/v8/postlist/w/search" {
		t.Fatalf("url = %q", rawURL)
	}
	if headers["content-type"] != "application/json" {
		t.Fatalf("content-type header = %q", headers["content-type"])
	}
	if body != `{"query":"two-bedroom"}` {
		t.Fatalf("body = %q", body)
	}
}

func TestValidateSearchCommand(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		command string
		wantErr bool
	}{
		{name: "valid", command: `curl 'https://api.divar.ir/v8/postlist/w/search'`, wantErr: false},
		{name: "insecure", command: `curl 'http://api.divar.ir/v8/postlist/w/search'`, wantErr: true},
		{name: "alternate host", command: `curl 'https://example.com/v8/postlist/w/search'`, wantErr: true},
		{name: "host suffix", command: `curl 'https://api.divar.ir.example.com/v8/postlist/w/search'`, wantErr: true},
		{name: "wrong path", command: `curl 'https://api.divar.ir/v8/posts-v2/web/token'`, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := ValidateSearchCommand(tt.command)
			if (err != nil) != tt.wantErr {
				t.Fatalf("ValidateSearchCommand() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestExtractImportantDataIncludesGenericAttributes(t *testing.T) {
	t.Parallel()

	post := PostDetailRes{
		Webengage: Webengage{BrandModel: "Pride", Price: 500000000},
		Sections: []Section{{
			SectionName: "LIST_DATA",
			Widgets: []Widget{{
				WidgetType: "GROUP_INFO_ROW",
				Data: WidgetData{Items: []Item{
					{Title: "کارکرد", Value: "120000"},
					{Title: "رنگ", Value: "سفید"},
				}},
			}},
		}},
	}

	data := ExtractImportantData(post)
	if data.Attributes["brand_model"] != "Pride" || data.Attributes["کارکرد"] != "120000" || data.Attributes["رنگ"] != "سفید" {
		t.Fatalf("attributes = %#v", data.Attributes)
	}
	if data.Attributes["price"] != "500000000" {
		t.Fatalf("price attribute = %q", data.Attributes["price"])
	}
}
