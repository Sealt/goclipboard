package model

import (
	"strings"
	"testing"
	"time"

	"goclipboard/internal/crdt"
)

func TestValidateKey(t *testing.T) {
	valid := []string{
		"abc123",
		"AbC._-9",
		"a",
		"0",
		"room-name_1.2",
	}
	for _, key := range valid {
		if _, err := ValidateKey(key); err != nil {
			t.Errorf("ValidateKey(%q) unexpected error: %v", key, err)
		}
	}

	invalid := []string{
		"",
		"api", "API", "Api", // reserved, case-insensitive
		"static", "healthz",
		"a/b",
		"a b",
		"-leading",
		"_leading",
		".leading",
		strings.Repeat("x", 65), // > 64 chars
		"汉字",
	}
	for _, key := range invalid {
		if _, err := ValidateKey(key); err == nil {
			t.Errorf("ValidateKey(%q) expected error, got nil", key)
		}
	}
}

func TestTTLFromSeconds(t *testing.T) {
	if d, err := TTLFromSeconds(3600); err != nil || d != time.Hour {
		t.Fatalf("TTLFromSeconds(3600) = %v, %v; want 1h", d, err)
	}

	bad := []int64{0, -1, 1<<63 - 1, 1 << 40}
	for _, s := range bad {
		if _, err := TTLFromSeconds(s); err == nil {
			t.Errorf("TTLFromSeconds(%d) expected error, got nil", s)
		}
	}
}

func TestResponseFromClipboard(t *testing.T) {
	doc, err := crdt.BuildFromString("s", "hi")
	if err != nil {
		t.Fatal(err)
	}
	expires := time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC)
	item := Clipboard{
		Doc:        doc,
		Content:    "hi",
		TTL:        time.Hour,
		ExpiresAt:  expires,
		Version:    7,
		Generation: 3,
		UpdatedAt:  expires,
		UpdatedBy:  "alice",
	}
	resp := ResponseFromClipboard("k1", item, true)
	if resp.Key != "k1" || resp.Content != "hi" || resp.TTLSeconds != 3600 {
		t.Errorf("basic fields wrong: %+v", resp)
	}
	if resp.ExpiresAt != "2026-08-01T12:00:00Z" {
		t.Errorf("ExpiresAt = %q, want RFC3339 UTC", resp.ExpiresAt)
	}
	if resp.Version != 7 || resp.Generation != 3 || !resp.Exists || resp.UpdatedBy != "alice" {
		t.Errorf("version/generation/exists/updatedBy wrong: %+v", resp)
	}
}
