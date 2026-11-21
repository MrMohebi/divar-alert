package divar

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math/big"
	"strconv"
)

// FilterNumberRange uses decimal integer strings to match Divar's int64 JSON
// encoding without losing precision. Empty bounds are omitted; "0" is retained.
type FilterNumberRange struct {
	Minimum string `json:"minimum,omitempty"`
	Maximum string `json:"maximum,omitempty"`
}

// UnmarshalJSON accepts either quoted bounds or JSON numbers from AI providers.
// Decode raw number text directly, never through float64: currency values and
// other int64 ranges must keep their exact value. Marshaling still emits strings
// as required by Divar's protobuf JSON representation.
func (bounds *FilterNumberRange) UnmarshalJSON(data []byte) error {
	data = bytes.TrimSpace(data)
	if len(data) == 0 || data[0] != '{' {
		return fmt.Errorf("number range must be an object")
	}
	var raw struct {
		Minimum json.RawMessage `json:"minimum"`
		Maximum json.RawMessage `json:"maximum"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	decodeBound := func(data json.RawMessage) (string, error) {
		data = bytes.TrimSpace(data)
		if len(data) == 0 || bytes.Equal(data, []byte("null")) {
			return "", nil
		}
		if data[0] == '"' {
			var value string
			err := json.Unmarshal(data, &value)
			return value, err
		}
		var value json.Number
		if err := json.Unmarshal(data, &value); err != nil {
			return "", fmt.Errorf("range bound must be a string or integer: %w", err)
		}
		number, ok := new(big.Rat).SetString(value.String())
		if !ok || !number.IsInt() || !number.Num().IsInt64() {
			return "", fmt.Errorf("range bound must be an exact int64 integer")
		}
		return number.Num().String(), nil
	}
	minimum, err := decodeBound(raw.Minimum)
	if err != nil {
		return fmt.Errorf("minimum: %w", err)
	}
	maximum, err := decodeBound(raw.Maximum)
	if err != nil {
		return fmt.Errorf("maximum: %w", err)
	}
	*bounds = FilterNumberRange{Minimum: minimum, Maximum: maximum}
	return nil
}

// EncodeValue converts a selection to Divar's form_data.data[key] wire value.
// Pass string, bool, []string, or FilterNumberRange according to ValueType.
// This checks wire types and range ordering, not every server-provided validator.
// Lazy options must be loaded separately before presenting all available values.
func (filter FilterDefinition) EncodeValue(value any) (json.RawMessage, error) {
	var encoded any
	switch filter.ValueType {
	case "str":
		v, ok := value.(string)
		if !ok {
			return nil, fmt.Errorf("filter %q requires a string", filter.Key)
		}
		encoded = map[string]string{"value": v}
	case "boolean":
		v, ok := value.(bool)
		if !ok {
			return nil, fmt.Errorf("filter %q requires a bool", filter.Key)
		}
		encoded = map[string]bool{"value": v}
	case "repeated_string":
		v, ok := value.([]string)
		if !ok {
			return nil, fmt.Errorf("filter %q requires []string", filter.Key)
		}
		if v == nil {
			v = []string{}
		}
		encoded = map[string][]string{"value": v}
	case "number_range":
		v, ok := value.(FilterNumberRange)
		if !ok {
			return nil, fmt.Errorf("filter %q requires FilterNumberRange", filter.Key)
		}
		if v.Minimum == "" && v.Maximum == "" {
			return nil, fmt.Errorf("filter %q requires at least one range bound; use nil to clear it", filter.Key)
		}
		var minimum, maximum int64
		for _, bound := range []struct {
			value  string
			target *int64
		}{{v.Minimum, &minimum}, {v.Maximum, &maximum}} {
			if bound.value != "" {
				n, err := strconv.ParseInt(bound.value, 10, 64)
				if err != nil {
					return nil, fmt.Errorf("filter %q has an invalid integer bound %q", filter.Key, bound.value)
				}
				*bound.target = n
			}
		}
		if v.Minimum != "" && v.Maximum != "" && minimum > maximum {
			return nil, fmt.Errorf("filter %q minimum exceeds maximum", filter.Key)
		}
		encoded = v
	default:
		return nil, fmt.Errorf("filter %q has unsupported value type %q", filter.Key, filter.ValueType)
	}
	return json.Marshal(map[string]any{filter.ValueType: encoded})
}

// BuildFormData merges selections with the current search form, preserving its
// category and existing values. A nil selection clears that filter. The returned
// map can be assigned to a search request's form_data.data or FiltersParams.FormData.
func (result FiltersResult) BuildFormData(selections map[string]any) (FilterValues, error) {
	data := cloneFormData(result.SearchData.FormData.Data)
	filters := make(map[string]FilterDefinition, len(result.Filters))
	for _, filter := range result.Filters {
		filters[filter.Key] = filter
	}
	for key, value := range selections {
		filter, ok := filters[key]
		if !ok {
			return nil, fmt.Errorf("unknown filter %q for category %q", key, result.Category)
		}
		if value == nil {
			delete(data, key)
			continue
		}
		encoded, err := filter.EncodeValue(value)
		if err != nil {
			return nil, err
		}
		data[key] = encoded
	}
	return data, nil
}
