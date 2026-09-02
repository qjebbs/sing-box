package option

import (
	"encoding/json"
	"testing"
)

func TestHealthCheckOptionsJSON(t *testing.T) {
	var o HealthCheckOptions
	// tag 引用
	if err := json.Unmarshal([]byte(`"my-tag"`), &o); err != nil {
		t.Fatal(err)
	}
	if o.Tag != "my-tag" {
		t.Fatalf("expected tag my-tag, got %q", o.Tag)
	}
	// 内联配置
	var o2 HealthCheckOptions
	if err := json.Unmarshal([]byte(`{"interval": "5m", "destination": "https://example.com", "detour_of": ["a"]}`), &o2); err != nil {
		t.Fatal(err)
	}
	if o2.Tag != "" {
		t.Fatalf("expected empty tag, got %q", o2.Tag)
	}
	if o2.Destination != "https://example.com" {
		t.Fatalf("unexpected destination %q", o2.Destination)
	}
	if o2.IsEmpty() {
		t.Fatal("inline options should not be empty")
	}
	// 顶层 HealthCheck 对象
	var h HealthCheck
	if err := json.Unmarshal([]byte(`{"tag": "x", "destination": "https://x.com"}`), &h); err != nil {
		t.Fatal(err)
	}
	if h.Tag != "x" {
		t.Fatalf("expected tag x, got %q", h.Tag)
	}
	if h.Destination != "https://x.com" {
		t.Fatalf("unexpected destination %q", h.Destination)
	}
}
