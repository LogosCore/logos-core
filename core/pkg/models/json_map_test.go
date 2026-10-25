package models

import "testing"

// Endpoint values hold nested objects (string_map fields): they must come
// back as plain JSON shapes, not BSON documents.
func TestJSONMapRoundTripsNestedObjects(t *testing.T) {
	in := JSONMap{"headers": map[string]any{"X-A": "1"}, "base_url": "https://a.example.com"}
	raw, err := in.MarshalBSON()
	if err != nil {
		t.Fatal(err)
	}
	var out JSONMap
	if err := out.UnmarshalBSON(raw); err != nil {
		t.Fatal(err)
	}
	headers, ok := out["headers"].(map[string]any)
	if !ok || headers["X-A"] != "1" || out["base_url"] != "https://a.example.com" {
		t.Errorf("round trip = %#v", out)
	}
}
