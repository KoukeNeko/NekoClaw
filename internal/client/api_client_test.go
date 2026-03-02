package client

import "testing"

func TestDecodeAPIErrorStructuredObject(t *testing.T) {
	err := decodeAPIError(400, []byte(`{"error":{"code":"missing_project","message":"project is required"}}`))
	if err == nil {
		t.Fatalf("expected error")
	}
	got := err.Error()
	want := "api error (400): missing_project: project is required"
	if got != want {
		t.Fatalf("unexpected error: %q want %q", got, want)
	}
}

func TestDecodeAPIErrorString(t *testing.T) {
	err := decodeAPIError(502, []byte(`{"error":"bad gateway"}`))
	if err == nil {
		t.Fatalf("expected error")
	}
	got := err.Error()
	want := "api error (502): bad gateway"
	if got != want {
		t.Fatalf("unexpected error: %q want %q", got, want)
	}
}
