package model

import (
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

func TestValidateFileID(t *testing.T) {
	valid := []string{
		"a1b2c3d4",                       // 8 hex
		strings.Repeat("a", 32),           // 32 hex
		" 0a1b2c3d4e5f6071 ",             // whitespace trimmed
	}
	for _, id := range valid {
		got, err := ValidateFileID(id)
		if err != nil || got != strings.TrimSpace(id) {
			t.Errorf("ValidateFileID(%q) = %q, %v; want trimmed ok", id, got, err)
		}
	}

	invalid := []string{
		"",
		"abc",          // too short
		"ABC12345",     // uppercase not allowed
		"xyz12345",     // non-hex
		strings.Repeat("a", 33), // too long
		"a1b2c3d4!",
	}
	for _, id := range invalid {
		if _, err := ValidateFileID(id); err == nil {
			t.Errorf("ValidateFileID(%q) expected error, got nil", id)
		}
	}
}

func TestSanitizeFileName(t *testing.T) {
	cases := []struct {
		in   string
		want string
		err  bool
	}{
		{"file.txt", "file.txt", false},
		{"../etc/passwd", "passwd", false},   // path traversal stripped
		{"/tmp/x/y", "y", false},
		{`a\b\c`, "abc", false},
		{"file\x00name.txt", "filename.txt", false}, // control char dropped
		{"   ", "", true},
		{"", "", true},
		{".", "", true},
		{"..", "", true},
		{"/", "", true},
	}
	for _, c := range cases {
		got, err := SanitizeFileName(c.in)
		if c.err {
			if err == nil {
				t.Errorf("SanitizeFileName(%q) expected error, got %q", c.in, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("SanitizeFileName(%q) unexpected error: %v", c.in, err)
			continue
		}
		if got != c.want {
			t.Errorf("SanitizeFileName(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestSanitizeFileNameLong(t *testing.T) {
	// 250 ASCII bytes → capped at 120 runes (120 bytes, ≤ 200-byte cap).
	long := strings.Repeat("x", 250)
	got, err := SanitizeFileName(long)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 120 {
		t.Fatalf("long ascii: len = %d, want 120", len(got))
	}

	// 100 multibyte runes (3 bytes each = 300 bytes) → capped to fit 200
	// bytes, and must stay valid UTF-8 (no rune split).
	longRunes := strings.Repeat("汉", 100)
	got, err = SanitizeFileName(longRunes)
	if err != nil {
		t.Fatal(err)
	}
	if !utf8.ValidString(got) {
		t.Fatalf("long runes: result is invalid UTF-8: %q", got)
	}
	if len(got) > 200 {
		t.Fatalf("long runes: %d bytes, want ≤ 200", len(got))
	}
	if len([]rune(got)) != 66 { // 66 × 3 = 198 bytes
		t.Fatalf("long runes: rune count = %d, want 66", len([]rune(got)))
	}
}

func TestSanitizeFileNameInvalidUTF8(t *testing.T) {
	if _, err := SanitizeFileName(string([]byte{0xff, 0xfe, 'a'})); err == nil {
		t.Error("expected error for invalid UTF-8 name")
	}
}

func TestFileInfoFrom(t *testing.T) {
	expires := time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC)
	info := FileInfoFrom(File{
		ID:          "a1b2c3d4",
		Name:        "x.bin",
		ContentType: "application/octet-stream",
		Size:        0, // rely on Data fallback
		Data:        []byte("hello"),
		ExpiresAt:   expires,
		UploadedAt:  expires,
	})
	if info.Size != 5 {
		t.Errorf("Size = %d, want 5 (from Data)", info.Size)
	}
	if info.ExpiresAt != "2026-08-01T12:00:00Z" {
		t.Errorf("ExpiresAt = %q, want RFC3339 UTC", info.ExpiresAt)
	}
}
