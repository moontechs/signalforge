package openrouter

import (
	"encoding/json"
	"fmt"
)

// validateResponse checks that content is valid JSON and, when schema is
// non-nil, that it can be unmarshaled into the schema type and that
// numeric fields are within expected bounds (hints 0-10, relevance 0-1).
func (c *Client) validateResponse(content string, schema any) ([]byte, error) {
	if !json.Valid([]byte(content)) {
		return nil, fmt.Errorf("%w: invalid JSON", ErrInvalidResponse)
	}

	if schema != nil {
		// Verify we can unmarshal into the schema type.
		if err := json.Unmarshal([]byte(content), schema); err != nil {
			return nil, fmt.Errorf("%w: schema validation: %s", ErrInvalidResponse, err)
		}

		// Check numeric field ranges.
		var raw map[string]any
		if uErr := json.Unmarshal([]byte(content), &raw); uErr == nil {
			if err := validateRanges(raw); err != nil {
				return nil, fmt.Errorf("%w: %s", ErrInvalidResponse, err)
			}
		}
	}

	return []byte(content), nil
}

// validateRanges checks that numeric fields in the JSON response are
// within their expected bounds.
func validateRanges(data map[string]any) error {
	// Hint fields must be in [0, 10].
	hintFields := []string{"severity_hint", "frequency_hint", "payment_hint", "frustration_hint"}
	for _, field := range hintFields {
		if v, ok := data[field]; ok {
			f, convOK := toFloat64(v)
			if convOK && (f < 0 || f > 10) {
				return fmt.Errorf("%s out of range [0,10]: %v", field, v)
			}
		}
	}

	// Relevance must be in [0, 1].
	if v, ok := data["relevance"]; ok {
		f, convOK := toFloat64(v)
		if convOK && (f < 0 || f > 1) {
			return fmt.Errorf("relevance out of range [0,1]: %v", v)
		}
	}

	return nil
}

// toFloat64 converts common JSON numeric types to float64.
func toFloat64(v any) (float64, bool) {
	switch val := v.(type) {
	case float64:
		return val, true
	case int:
		return float64(val), true
	case int64:
		return float64(val), true
	case json.Number:
		f, err := val.Float64()
		return f, err == nil
	default:
		return 0, false
	}
}
