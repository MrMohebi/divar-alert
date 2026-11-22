package divar

import (
	"encoding/json"
	"testing"
)

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

func TestExtractImportantDataReadsExactAndApproximateLocations(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		mapJSON  string
		lat, lng float64
		radius   float64
	}{
		{"exact", `{"type":"EXACT","exact_data":{"point":{"latitude":30.26,"longitude":57.03}}}`, 30.26, 57.03, 0},
		{"approximate", `{"type":"FUZZY","fuzzy_data":{"point":{"latitude":30.29,"longitude":57.04},"radius":500}}`, 30.29, 57.04, 500},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var post PostDetailRes
			raw := `{"sections":[{"section_name":"MAP","widgets":[{"widget_type":"MAP_ROW","data":{"location":` + tt.mapJSON + `}}]}]}`
			if err := json.Unmarshal([]byte(raw), &post); err != nil {
				t.Fatal(err)
			}
			got := ExtractImportantData(post).Location
			if !got.HasPoint() || got.Latitude != tt.lat || got.Longitude != tt.lng || got.RadiusMeters != tt.radius {
				t.Fatalf("location = %+v", got)
			}
		})
	}
	if (LocationInfo{}).HasPoint() {
		t.Fatal("empty location reported a point")
	}
}

func TestExtractImportantDataReadsRentTerms(t *testing.T) {
	t.Parallel()

	slider := PostDetailRes{
		Webengage: Webengage{Credit: 7500000256, Rent: 1000000},
		Sections: []Section{{
			SectionName: "LIST_DATA",
			Widgets: []Widget{{
				WidgetType: "RENT_SLIDER",
				Data: WidgetData{
					Credit: &Credit{Value: "7500000000", TransformedValue: "6000000000"},
					Rent:   &Credit{Value: "1000000", TransformedValue: "45000000"},
				},
			}},
		}},
	}
	if got := ExtractImportantData(slider).Price; got != (RentalPriceInfo{Deposit: "7500000000", Rent: "1000000", TransDeposit: "6000000000", TransRent: "45000000"}) {
		t.Fatalf("slider price = %+v", got)
	}

	rows := PostDetailRes{
		Webengage: Webengage{Credit: 2200000000},
		Sections: []Section{{
			SectionName: "LIST_DATA",
			Widgets: []Widget{
				{WidgetType: "UNEXPANDABLE_ROW", Data: WidgetData{Title: "ودیعه", Value: "‏۲,۲۰۰,۰۰۰,۰۰۰ تومان"}},
				{WidgetType: "UNEXPANDABLE_ROW", Data: WidgetData{Title: "اجارهٔ ماهانه", Value: "رایگان"}},
			},
		}},
	}
	if got := ExtractImportantData(rows).Price; got.Deposit != "‏۲,۲۰۰,۰۰۰,۰۰۰ تومان" || got.Rent != "رایگان" {
		t.Fatalf("row price = %+v", got)
	}
}
