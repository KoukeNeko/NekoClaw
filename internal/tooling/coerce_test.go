package tooling

import (
	"encoding/json"
	"testing"
)

func TestCoerceToolArguments_StringToInt(t *testing.T) {
	args := json.RawMessage(`{"line":"5","path":"/tmp/test"}`)
	schema := json.RawMessage(`{"type":"object","properties":{"line":{"type":"integer"},"path":{"type":"string"}}}`)

	result := coerceToolArguments(args, schema)

	var m map[string]any
	if err := json.Unmarshal(result, &m); err != nil {
		t.Fatal(err)
	}
	line, ok := m["line"]
	if !ok {
		t.Fatal("missing line")
	}
	// json.Unmarshal decodes numbers as float64 by default
	if f, ok := line.(float64); !ok || f != 5 {
		t.Errorf("expected line=5, got %v (%T)", line, line)
	}
	if path, _ := m["path"].(string); path != "/tmp/test" {
		t.Errorf("expected path=/tmp/test, got %v", path)
	}
}

func TestCoerceToolArguments_StringToFloat(t *testing.T) {
	args := json.RawMessage(`{"value":"3.14"}`)
	schema := json.RawMessage(`{"type":"object","properties":{"value":{"type":"number"}}}`)

	result := coerceToolArguments(args, schema)

	var m map[string]any
	if err := json.Unmarshal(result, &m); err != nil {
		t.Fatal(err)
	}
	if v, ok := m["value"].(float64); !ok || v != 3.14 {
		t.Errorf("expected 3.14, got %v", m["value"])
	}
}

func TestCoerceToolArguments_StringToBool(t *testing.T) {
	args := json.RawMessage(`{"recursive":"true","verbose":"false"}`)
	schema := json.RawMessage(`{"type":"object","properties":{"recursive":{"type":"boolean"},"verbose":{"type":"boolean"}}}`)

	result := coerceToolArguments(args, schema)

	var m map[string]any
	if err := json.Unmarshal(result, &m); err != nil {
		t.Fatal(err)
	}
	if v, ok := m["recursive"].(bool); !ok || !v {
		t.Errorf("expected recursive=true, got %v", m["recursive"])
	}
	if v, ok := m["verbose"].(bool); !ok || v {
		t.Errorf("expected verbose=false, got %v", m["verbose"])
	}
}

func TestCoerceToolArguments_FloatToInt(t *testing.T) {
	args := json.RawMessage(`{"count":5.0}`)
	schema := json.RawMessage(`{"type":"object","properties":{"count":{"type":"integer"}}}`)

	result := coerceToolArguments(args, schema)

	var m map[string]any
	if err := json.Unmarshal(result, &m); err != nil {
		t.Fatal(err)
	}
	// After coercion int64 gets re-marshalled then unmarshalled as float64
	if v, ok := m["count"].(float64); !ok || v != 5 {
		t.Errorf("expected count=5, got %v", m["count"])
	}
}

func TestCoerceToolArguments_AlreadyCorrect(t *testing.T) {
	args := json.RawMessage(`{"path":"/tmp","count":5}`)
	schema := json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"},"count":{"type":"integer"}}}`)

	result := coerceToolArguments(args, schema)

	// count is already a number in JSON, path is already a string
	var m map[string]any
	if err := json.Unmarshal(result, &m); err != nil {
		t.Fatal(err)
	}
	if m["path"] != "/tmp" {
		t.Errorf("expected /tmp, got %v", m["path"])
	}
}

func TestCoerceToolArguments_NoSchema(t *testing.T) {
	args := json.RawMessage(`{"x":"1"}`)
	result := coerceToolArguments(args, nil)
	if string(result) != `{"x":"1"}` {
		t.Errorf("expected original args, got %s", result)
	}
}

func TestCoerceToolArguments_EmptyArgs(t *testing.T) {
	schema := json.RawMessage(`{"type":"object","properties":{"x":{"type":"integer"}}}`)
	result := coerceToolArguments(nil, schema)
	if result != nil {
		t.Errorf("expected nil, got %s", result)
	}
}

func TestCoerceToolArguments_MalformedArgs(t *testing.T) {
	args := json.RawMessage(`not json`)
	schema := json.RawMessage(`{"type":"object","properties":{"x":{"type":"integer"}}}`)
	result := coerceToolArguments(args, schema)
	if string(result) != `not json` {
		t.Errorf("expected original, got %s", result)
	}
}

func TestCoerceToolArguments_NumberToString(t *testing.T) {
	args := json.RawMessage(`{"name":42}`)
	schema := json.RawMessage(`{"type":"object","properties":{"name":{"type":"string"}}}`)

	result := coerceToolArguments(args, schema)

	var m map[string]any
	if err := json.Unmarshal(result, &m); err != nil {
		t.Fatal(err)
	}
	if v, ok := m["name"].(string); !ok || v != "42" {
		t.Errorf("expected name=\"42\", got %v (%T)", m["name"], m["name"])
	}
}

func TestCoerceToolArguments_BoolToString(t *testing.T) {
	args := json.RawMessage(`{"flag":true}`)
	schema := json.RawMessage(`{"type":"object","properties":{"flag":{"type":"string"}}}`)

	result := coerceToolArguments(args, schema)

	var m map[string]any
	if err := json.Unmarshal(result, &m); err != nil {
		t.Fatal(err)
	}
	if v, ok := m["flag"].(string); !ok || v != "true" {
		t.Errorf("expected flag=\"true\", got %v (%T)", m["flag"], m["flag"])
	}
}

func TestCoerceToolArguments_UnknownFieldUntouched(t *testing.T) {
	args := json.RawMessage(`{"unknown":"value","count":"3"}`)
	schema := json.RawMessage(`{"type":"object","properties":{"count":{"type":"integer"}}}`)

	result := coerceToolArguments(args, schema)

	var m map[string]any
	if err := json.Unmarshal(result, &m); err != nil {
		t.Fatal(err)
	}
	if m["unknown"] != "value" {
		t.Errorf("expected unknown field untouched, got %v", m["unknown"])
	}
	if v, ok := m["count"].(float64); !ok || v != 3 {
		t.Errorf("expected count=3, got %v", m["count"])
	}
}
