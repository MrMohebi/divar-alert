package divar

import (
	"encoding/json"
	"testing"
)

func TestNumberRangeAcceptsNumericAndQuotedBoundsExactly(t *testing.T) {
	for _, tc := range []struct{ name, raw, min, max string }{
		{"numbers", `{"minimum":1000000000,"maximum":3000000000}`, "1000000000", "3000000000"},
		{"strings", `{"minimum":"1000000000","maximum":"3000000000"}`, "1000000000", "3000000000"},
		{"mixed", `{"minimum":1000000000,"maximum":"3000000000"}`, "1000000000", "3000000000"},
		{"beyond float precision", `{"minimum":9007199254740993,"maximum":9223372036854775807}`, "9007199254740993", "9223372036854775807"},
		{"scientific integer", `{"minimum":1e9,"maximum":3.0e9}`, "1000000000", "3000000000"},
		{"zero and omitted", `{"minimum":0}`, "0", ""},
		{"negative floor", `{"minimum":-2,"maximum":3}`, "-2", "3"},
		{"null optional bound", `{"minimum":null,"maximum":3000000000}`, "", "3000000000"},
		{"Persian phrase", `{"maximum":"۱.۵ میلیارد تومان"}`, "", "۱.۵ میلیارد تومان"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var bounds FilterNumberRange
			if err := json.Unmarshal([]byte(tc.raw), &bounds); err != nil {
				t.Fatal(err)
			}
			if bounds.Minimum != tc.min || bounds.Maximum != tc.max {
				t.Fatalf("bounds=%+v", bounds)
			}
			data, err := json.Marshal(bounds)
			if err != nil {
				t.Fatal(err)
			}
			var encoded map[string]any
			if err = json.Unmarshal(data, &encoded); err != nil {
				t.Fatal(err)
			}
			for _, value := range encoded {
				if _, ok := value.(string); !ok {
					t.Fatal("Divar wire bounds must remain strings")
				}
			}
		})
	}
}

func TestNumberRangeRejectsInvalidNumericBounds(t *testing.T) {
	for _, raw := range []string{`null`, `[]`, `{"minimum":false}`, `{"minimum":[]}`, `{"minimum":{}}`, `{"minimum":1.5}`, `{"maximum":9223372036854775808}`, `{"maximum":1e100}`} {
		var bounds FilterNumberRange
		if err := json.Unmarshal([]byte(raw), &bounds); err == nil {
			t.Fatalf("invalid range accepted: %s", raw)
		}
	}
}
