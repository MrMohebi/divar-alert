package divar

import (
	"encoding/json"
	"fmt"
	"strconv"
)

// DecodeSelection decodes a plain model/user selection, checks known options and
// Divar's numeric constraints, and leaves encoding to BuildFormData.
func (f FilterDefinition) DecodeSelection(raw json.RawMessage) (any, error) {
	var value any
	switch f.ValueType {
	case "str":
		var v string
		if err := json.Unmarshal(raw, &v); err != nil {
			return nil, err
		}
		value = v
	case "boolean":
		var v bool
		if err := json.Unmarshal(raw, &v); err != nil {
			return nil, err
		}
		value = v
	case "repeated_string":
		var v []string
		if err := json.Unmarshal(raw, &v); err != nil {
			return nil, err
		}
		value = v
	case "number_range":
		var v FilterNumberRange
		if err := json.Unmarshal(raw, &v); err != nil {
			return nil, err
		}
		value = v
	default:
		return nil, fmt.Errorf("unsupported filter type %q", f.ValueType)
	}
	if string(raw) == "null" {
		return nil, fmt.Errorf("null is not a selection")
	}
	if _, err := f.EncodeValue(value); err != nil {
		return nil, err
	}
	allowed := map[string]bool{}
	var collect func([]FilterOption)
	collect = func(options []FilterOption) {
		for _, o := range options {
			allowed[o.Value] = true
			collect(o.Children)
		}
	}
	collect(f.Options)
	check := func(v string) error {
		if len(allowed) > 0 && !allowed[v] {
			return fmt.Errorf("invalid option %q for %q", v, f.Key)
		}
		return nil
	}
	switch v := value.(type) {
	case string:
		if err := check(v); err != nil {
			return nil, err
		}
	case []string:
		for _, s := range v {
			if err := check(s); err != nil {
				return nil, err
			}
		}
	}
	if v, ok := value.(FilterNumberRange); ok {
		var validators map[string][]map[string]json.RawMessage
		if len(f.Validators) > 0 {
			if err := json.Unmarshal(f.Validators, &validators); err != nil {
				return nil, err
			}
		}
		for _, b := range []struct{ value, key string }{{v.Minimum, "minimum_validators"}, {v.Maximum, "maximum_validators"}} {
			for _, rule := range validators[b.key] {
				for kind, data := range rule {
					var spec struct {
						Min    string   `json:"min"`
						Max    string   `json:"max"`
						Values []string `json:"values"`
					}
					if err := json.Unmarshal(data, &spec); err != nil {
						return nil, err
					}
					if b.value == "" {
						if kind == "required" {
							return nil, fmt.Errorf("%s requires a bound", f.Key)
						}
						continue
					}
					n, _ := strconv.ParseInt(b.value, 10, 64)
					switch kind {
					case "min":
						s := spec.Min
						if s == "" {
							s = "0"
						}
						limit, err := strconv.ParseInt(s, 10, 64)
						if err != nil || n < limit {
							return nil, fmt.Errorf("%s below minimum", f.Key)
						}
					case "max":
						s := spec.Max
						if s == "" {
							s = "0"
						}
						limit, err := strconv.ParseInt(s, 10, 64)
						if err != nil || n > limit {
							return nil, fmt.Errorf("%s above maximum", f.Key)
						}
					case "contain":
						found := false
						for _, s := range spec.Values {
							if s == b.value {
								found = true
							}
						}
						if !found {
							return nil, fmt.Errorf("%s has unsupported bound", f.Key)
						}
					case "required":
					default:
						return nil, fmt.Errorf("unsupported numeric validator %q", kind)
					}
				}
			}
		}
		for _, rule := range validators["number_range_validators"] {
			for kind := range rule {
				if kind != "min_must_be_less_or_equal_max" && kind != "min_must_be_less_than_max" {
					return nil, fmt.Errorf("unsupported range validator %q", kind)
				}
				if kind == "min_must_be_less_than_max" && v.Minimum != "" && v.Maximum != "" {
					lo, _ := strconv.ParseInt(v.Minimum, 10, 64)
					hi, _ := strconv.ParseInt(v.Maximum, 10, 64)
					if lo >= hi {
						return nil, fmt.Errorf("%s requires increasing bounds", f.Key)
					}
				}
			}
		}
	}
	return value, nil
}
