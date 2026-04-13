package tooling

import (
	"encoding/json"
	"math"
	"strconv"
	"strings"
)

// coerceToolArguments adjusts argument values to match the types declared
// in the tool's JSON Schema. Open-source models frequently send wrong types
// (e.g. string "5" instead of integer 5). This function coerces values
// according to the schema and returns corrected JSON. On any parse failure
// it returns the original arguments unchanged.
func coerceToolArguments(args json.RawMessage, schema json.RawMessage) json.RawMessage {
	if len(args) == 0 || len(schema) == 0 {
		return args
	}

	propTypes := schemaPropertyTypes(schema)
	if len(propTypes) == 0 {
		return args
	}

	var argsMap map[string]any
	if err := json.Unmarshal(args, &argsMap); err != nil {
		return args
	}

	changed := false
	for key, val := range argsMap {
		expectedType, ok := propTypes[key]
		if !ok || val == nil {
			continue
		}
		coerced, didCoerce := coerceValue(val, expectedType)
		if didCoerce {
			argsMap[key] = coerced
			changed = true
		}
	}

	if !changed {
		return args
	}

	result, err := json.Marshal(argsMap)
	if err != nil {
		return args
	}
	return result
}

// schemaPropertyTypes extracts a map of property names to their declared
// JSON Schema type from a schema object.
func schemaPropertyTypes(schema json.RawMessage) map[string]string {
	var s struct {
		Properties map[string]struct {
			Type string `json:"type"`
		} `json:"properties"`
	}
	if err := json.Unmarshal(schema, &s); err != nil {
		return nil
	}
	result := make(map[string]string, len(s.Properties))
	for name, prop := range s.Properties {
		if prop.Type != "" {
			result[name] = prop.Type
		}
	}
	return result
}

// coerceValue attempts to coerce a single value to the expected JSON Schema
// type. Returns the coerced value and true if a conversion was performed,
// or the original value and false if no coercion was needed or possible.
func coerceValue(val any, expectedType string) (any, bool) {
	switch expectedType {
	case "integer":
		return coerceToInteger(val)
	case "number":
		return coerceToNumber(val)
	case "boolean":
		return coerceToBoolean(val)
	case "string":
		return coerceToString(val)
	default:
		return val, false
	}
}

func coerceToInteger(val any) (any, bool) {
	switch v := val.(type) {
	case string:
		i, err := strconv.ParseInt(strings.TrimSpace(v), 10, 64)
		if err != nil {
			return val, false
		}
		return i, true
	case float64:
		if v == math.Trunc(v) && !math.IsInf(v, 0) && !math.IsNaN(v) {
			return int64(v), true
		}
		return val, false
	case json.Number:
		i, err := v.Int64()
		if err != nil {
			return val, false
		}
		return i, true
	default:
		return val, false
	}
}

func coerceToNumber(val any) (any, bool) {
	switch v := val.(type) {
	case string:
		f, err := strconv.ParseFloat(strings.TrimSpace(v), 64)
		if err != nil {
			return val, false
		}
		return f, true
	case json.Number:
		f, err := v.Float64()
		if err != nil {
			return val, false
		}
		return f, true
	default:
		return val, false
	}
}

func coerceToBoolean(val any) (any, bool) {
	switch v := val.(type) {
	case string:
		s := strings.TrimSpace(strings.ToLower(v))
		switch s {
		case "true", "1", "yes":
			return true, true
		case "false", "0", "no":
			return false, true
		}
		return val, false
	case float64:
		if v == 0 {
			return false, true
		}
		if v == 1 {
			return true, true
		}
		return val, false
	default:
		return val, false
	}
}

func coerceToString(val any) (any, bool) {
	switch v := val.(type) {
	case float64:
		if v == math.Trunc(v) && !math.IsInf(v, 0) {
			return strconv.FormatInt(int64(v), 10), true
		}
		return strconv.FormatFloat(v, 'f', -1, 64), true
	case bool:
		return strconv.FormatBool(v), true
	case json.Number:
		return v.String(), true
	default:
		return val, false
	}
}
