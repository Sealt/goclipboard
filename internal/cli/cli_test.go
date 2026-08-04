package cli

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// stubServer implements the clipboard API surface the CLI uses.
func stubServer(t *testing.T) *httptest.Server {
	t.Helper()
	var rooms = map[string]string{} // key -> content
	mux := http.NewServeMux()
	mux.HandleFunc("/api/clipboard", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		var body struct {
			Content string `json:"content"`
		}
		json.NewDecoder(r.Body).Decode(&body)
		key := "serverkey"
		rooms[key] = body.Content
		json.NewEncoder(w).Encode(map[string]any{
			"key": key, "content": body.Content, "ttlSeconds": 3600,
			"version": 1, "exists": true,
		})
	})
	mux.HandleFunc("/api/clipboard/", func(w http.ResponseWriter, r *http.Request) {
		key := strings.TrimPrefix(r.URL.Path, "/api/clipboard/")
		switch r.Method {
		case http.MethodPut:
			var body struct {
				Content string `json:"content"`
			}
			json.NewDecoder(r.Body).Decode(&body)
			rooms[key] = body.Content
			json.NewEncoder(w).Encode(map[string]any{
				"key": key, "content": body.Content, "ttlSeconds": 3600,
				"version": 1, "exists": true,
			})
		case http.MethodGet:
			content, ok := rooms[key]
			if !ok {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			json.NewEncoder(w).Encode(map[string]any{
				"key": key, "content": content, "ttlSeconds": 3600,
				"version": 1, "exists": true,
			})
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	})
	return httptest.NewServer(mux)
}

func runCLI(t *testing.T, stdin string, args ...string) (int, string, string) {
	t.Helper()
	var out, errBuf bytes.Buffer
	code := Run(args, strings.NewReader(stdin), &out, &errBuf)
	return code, out.String(), errBuf.String()
}

func TestPushStdin(t *testing.T) {
	srv := stubServer(t)
	defer srv.Close()

	code, out, errOut := runCLI(t, "hello world", "push", "-url", srv.URL)
	if code != ExitOK {
		t.Fatalf("push exit = %d, stderr: %s", code, errOut)
	}
	wantURL := srv.URL + "/serverkey"
	if strings.TrimSpace(out) != wantURL {
		t.Fatalf("push stdout = %q, want %q", out, wantURL)
	}
	if strings.TrimSpace(errOut) != "" {
		t.Fatalf("push stderr not empty: %q", errOut)
	}
}

func TestPushWithKeyAndFile(t *testing.T) {
	srv := stubServer(t)
	defer srv.Close()

	file := filepath.Join(t.TempDir(), "notes.txt")
	if err := os.WriteFile(file, []byte("from file"), 0o644); err != nil {
		t.Fatal(err)
	}
	code, out, errOut := runCLI(t, "ignored", "push", "-url", srv.URL, "-key", "custom", file)
	if code != ExitOK {
		t.Fatalf("push exit = %d, stderr: %s", code, errOut)
	}
	if strings.TrimSpace(out) != srv.URL+"/custom" {
		t.Fatalf("push stdout = %q", out)
	}
}

func TestPushRejectsOversize(t *testing.T) {
	srv := stubServer(t)
	defer srv.Close()
	big := strings.Repeat("x", MaxPushBytes+1)
	code, _, errOut := runCLI(t, big, "push", "-url", srv.URL)
	if code != ExitError {
		t.Fatalf("oversize push exit = %d, want %d; stderr: %s", code, ExitError, errOut)
	}
	if !strings.Contains(errOut, "1 MiB") {
		t.Fatalf("unexpected error text: %q", errOut)
	}
}

func TestPullByKeyAndByURL(t *testing.T) {
	srv := stubServer(t)
	defer srv.Close()

	// Seed a room via push.
	if code, _, errOut := runCLI(t, "pulled content", "push", "-url", srv.URL, "-key", "abc"); code != ExitOK {
		t.Fatalf("seed push failed: %s", errOut)
	}

	// Pull by bare key.
	code, out, errOut := runCLI(t, "", "pull", "-url", srv.URL, "abc")
	if code != ExitOK || strings.TrimSpace(out) != "pulled content" {
		t.Fatalf("pull by key: code=%d out=%q err=%q", code, out, errOut)
	}

	// Pull by full URL (query strings are ignored by the resolver).
	code, out, _ = runCLI(t, "", "pull", srv.URL+"/abc?x=1")
	if code != ExitOK || strings.TrimSpace(out) != "pulled content" {
		t.Fatalf("pull by url: code=%d out=%q", code, out)
	}
}

func TestPullToFile(t *testing.T) {
	srv := stubServer(t)
	defer srv.Close()
	if code, _, errOut := runCLI(t, "to file", "push", "-url", srv.URL, "-key", "k1"); code != ExitOK {
		t.Fatalf("seed: %s", errOut)
	}
	outFile := filepath.Join(t.TempDir(), "out.txt")
	code, out, errOut := runCLI(t, "", "pull", "-url", srv.URL, "-o", outFile, "k1")
	if code != ExitOK {
		t.Fatalf("pull exit = %d, stderr: %s", code, errOut)
	}
	if out != "" {
		t.Fatalf("pull -o wrote to stdout: %q", out)
	}
	data, err := os.ReadFile(outFile)
	if err != nil || string(data) != "to file" {
		t.Fatalf("file content = %q (err %v)", data, err)
	}
}

func TestParseTTL(t *testing.T) {
	cases := []struct {
		in   string
		want int64
		ok   bool
	}{
		{"3600", 3600, true},
		{"30m", 1800, true},
		{"2h", 7200, true},
		{"1d", 86400, true},
		{"2d", 172800, true},
		{"0", 0, false},
		{"-5", 0, false},
		{"bogus", 0, false},
	}
	for _, c := range cases {
		got, err := parseTTL(c.in)
		if (err == nil) != c.ok {
			t.Errorf("parseTTL(%q) err = %v, want ok=%v", c.in, err, c.ok)
			continue
		}
		if err == nil && got != c.want {
			t.Errorf("parseTTL(%q) = %d, want %d", c.in, got, c.want)
		}
	}
}

func TestUnknownCommand(t *testing.T) {
	code, _, errOut := runCLI(t, "", "frobnicate")
	if code != ExitUsage {
		t.Fatalf("exit = %d, want %d; stderr: %s", code, ExitUsage, errOut)
	}
	if !strings.Contains(errOut, "unknown command") {
		t.Fatalf("stderr = %q", errOut)
	}
}

func TestRunEmptyIsNotCLI(t *testing.T) {
	if code := Run(nil, strings.NewReader(""), &bytes.Buffer{}, &bytes.Buffer{}); code != ExitNotCLI {
		t.Fatalf("empty args should not be a CLI command, got %d", code)
	}
	if code := Run([]string{"serve"}, strings.NewReader(""), &bytes.Buffer{}, &bytes.Buffer{}); code != ExitNotCLI {
		t.Fatalf("serve should hand control to the server, got %d", code)
	}
}
