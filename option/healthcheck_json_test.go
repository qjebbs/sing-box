package option

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	sjson "github.com/sagernet/sing/common/json"
)

func TestHealthCheckOptionsJSON(t *testing.T) {
	var o HealthCheckOptions
	// tag 引用
	if err := sjson.UnmarshalContext(context.Background(), []byte(`"my-tag"`), &o); err != nil {
		t.Fatal(err)
	}
	if o.Tag != "my-tag" {
		t.Fatalf("expected tag my-tag, got %q", o.Tag)
	}
	// 内联配置
	var o2 HealthCheckOptions
	if err := json.Unmarshal([]byte(`{"interval": "5m", "url": "https://example.com", "relay_path": ["a"]}`), &o2); err != nil {
		t.Fatal(err)
	}
	if o2.Tag != "" {
		t.Fatalf("expected empty tag, got %q", o2.Tag)
	}
	if o2.URL != "https://example.com" {
		t.Fatalf("unexpected url %q", o2.URL)
	}
	if o2.IsEmpty() {
		t.Fatal("inline options should not be empty")
	}
	// 顶层 HealthCheck 对象
	var h HealthCheck
	if err := json.Unmarshal([]byte(`{"tag": "x", "url": "https://x.com"}`), &h); err != nil {
		t.Fatal(err)
	}
	if h.Tag != "x" {
		t.Fatalf("expected tag x, got %q", h.Tag)
	}
	if h.URL != "https://x.com" {
		t.Fatalf("unexpected url %q", h.URL)
	}
}

func TestHealthCheckOptionsRejectsUnknownField(t *testing.T) {
	var o HealthCheckOptions
	err := sjson.UnmarshalContext(context.Background(), []byte(`{"destination": "https://example.com"}`), &o)
	if err == nil {
		t.Fatal("expected an unknown field error")
	}
	if !strings.Contains(err.Error(), `unknown field "destination"`) {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestHealthCheckRejectsUnknownField(t *testing.T) {
	var h HealthCheck
	err := sjson.UnmarshalContext(context.Background(), []byte(`{"tag": "x", "destination": "https://example.com"}`), &h)
	if err == nil {
		t.Fatal("expected an unknown field error")
	}
	if !strings.Contains(err.Error(), `unknown field "destination"`) {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestHealthCheckOptionsTagReferenceContext(t *testing.T) {
	var o HealthCheckOptions
	if err := sjson.UnmarshalContext(context.Background(), []byte(`"my-tag"`), &o); err != nil {
		t.Fatal(err)
	}
	if o.Tag != "my-tag" {
		t.Fatalf("expected tag my-tag, got %q", o.Tag)
	}
}
