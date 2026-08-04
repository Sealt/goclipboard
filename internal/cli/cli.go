// Package cli implements the push/pull client modes of the goclipboard
// binary: the same single binary that serves rooms can also push content to
// and pull content from a (remote or local) instance:
//
//	echo "hello" | goclipboard push            # → prints the room URL
//	goclipboard push -ttl 2h notes.txt
//	goclipboard pull https://host/AbC123       # prints content
//	goclipboard pull -o out.txt AbC123
//
// The server URL comes from -url or GOCLIPBOARD_URL (default localhost:8080).
package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	DefaultBaseURL = "http://localhost:8080"
	// Matches the server's content cap (store.MaxContentBytes).
	MaxPushBytes = 1 << 20
)

// Exit codes.
const (
	ExitOK     = 0
	ExitError  = 1
	ExitUsage  = 2
	ExitNotCLI = -1 // not a CLI command → caller should start the server
)

// Run dispatches a CLI invocation. Returns ExitNotCLI when args do not name a
// CLI command (the caller starts the HTTP server instead).
func Run(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		return ExitNotCLI
	}
	switch args[0] {
	case "serve":
		return ExitNotCLI
	case "push":
		return runPush(args[1:], stdin, stdout, stderr)
	case "pull":
		return runPull(args[1:], stdout, stderr)
	case "-h", "--help", "help":
		printUsage(stderr)
		return ExitOK
	default:
		fmt.Fprintf(stderr, "goclipboard: unknown command %q (expected push | pull | serve)\n", args[0])
		printUsage(stderr)
		return ExitUsage
	}
}

func printUsage(w io.Writer) {
	fmt.Fprint(w, `goclipboard — ephemeral clipboard server + CLI client

Usage:
  goclipboard [serve]                 start the web server (default)
  goclipboard push [-url U] [-ttl D] [-key K] [-password P] [-v] [file]
      Push stdin (or file) to a room and print the room URL.
      -url       server base URL (env GOCLIPBOARD_URL, default http://localhost:8080)
      -ttl       lifetime: 30m, 2h, 1d, or plain seconds (default 1h)
      -key       room key to write; server generates one when omitted
      -password  edit password: unlocks locked rooms; on unlocked rooms claim-locks atomically with the write
      -v         also print the read-only view link (stderr)
  goclipboard pull [-url U] [-o file] [-password P] <url|key>
      Print room content to stdout (or write to -o file).
      -password  room password (required for view-protected rooms)
`)
}

// ---------------------------------------------------------------------------

type pushFlags struct {
	base string
	key  string
	ttl  string
	view bool
}

func runPush(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("push", flag.ContinueOnError)
	fs.SetOutput(stderr)
	base := fs.String("url", "", "server base URL (env GOCLIPBOARD_URL)")
	key := fs.String("key", "", "room key to write (default: server creates one)")
	ttl := fs.String("ttl", "1h", "lifetime: 30m, 2h, 1d or plain seconds")
	view := fs.Bool("v", false, "also print the read-only view link")
	password := fs.String("password", "", "edit password for locked rooms")
	fs.Usage = func() { printUsage(stderr) }
	if err := fs.Parse(args); err != nil {
		return ExitUsage
	}
	if fs.NArg() > 1 {
		fmt.Fprintln(stderr, "goclipboard push: at most one file argument")
		return ExitUsage
	}

	content, err := readInput(stdin, fs.Arg(0))
	if err != nil {
		fmt.Fprintf(stderr, "goclipboard push: %v\n", err)
		return ExitError
	}
	ttlSeconds, err := parseTTL(*ttl)
	if err != nil {
		fmt.Fprintf(stderr, "goclipboard push: invalid -ttl: %v\n", err)
		return ExitUsage
	}

	// password authorizes writes to locked rooms. setPassword claim-locks an
	// unlocked room under the same write so content never sits unlocked
	// between a POST/PUT and a follow-up /password (race with concurrent readers).
	payload := map[string]any{
		"content":    content,
		"ttlSeconds": ttlSeconds,
		"clientId":   "cli",
	}
	if pw := strings.TrimSpace(*password); pw != "" {
		payload["password"] = pw
		payload["setPassword"] = true
		payload["passwordScope"] = "edit"
	}
	body, _ := json.Marshal(payload)

	baseURL := baseURLOf(*base)
	var respURL string
	if *key != "" {
		u := baseURL + "/api/clipboard/" + *key
		code, data, err := doJSON(http.MethodPut, u, body)
		if err != nil {
			fmt.Fprintf(stderr, "goclipboard push: %v\n", err)
			return ExitError
		}
		if code != http.StatusOK {
			fmt.Fprintf(stderr, "goclipboard push: server %d: %s\n", code, data)
			return ExitError
		}
		respURL = baseURL + "/" + *key
		if *view {
			fmt.Fprintf(stderr, "view: %s?view=true\n", respURL)
		}
	} else {
		u := baseURL + "/api/clipboard"
		code, data, err := doJSON(http.MethodPost, u, body)
		if err != nil {
			fmt.Fprintf(stderr, "goclipboard push: %v\n", err)
			return ExitError
		}
		if code != http.StatusOK {
			fmt.Fprintf(stderr, "goclipboard push: server %d: %s\n", code, data)
			return ExitError
		}
		var created struct {
			Key     string `json:"key"`
			ViewKey string `json:"viewKey"`
		}
		if err := json.Unmarshal(data, &created); err != nil || created.Key == "" {
			fmt.Fprintf(stderr, "goclipboard push: unexpected server response: %s\n", data)
			return ExitError
		}
		respURL = baseURL + "/" + created.Key
		if *view {
			fmt.Fprintf(stderr, "view: %s?view=true\n", respURL)
		}
	}
	fmt.Fprintln(stdout, respURL)
	return ExitOK
}

// ---------------------------------------------------------------------------

func runPull(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("pull", flag.ContinueOnError)
	fs.SetOutput(stderr)
	base := fs.String("url", "", "server base URL (env GOCLIPBOARD_URL)")
	outFile := fs.String("o", "", "write content to this file instead of stdout")
	password := fs.String("password", "", "room password (required for view-protected rooms)")
	fs.Usage = func() { printUsage(stderr) }
	if err := fs.Parse(args); err != nil {
		return ExitUsage
	}
	if fs.NArg() != 1 {
		fmt.Fprintln(stderr, "goclipboard pull: exactly one <url|key> argument required")
		return ExitUsage
	}

	target := resolveTarget(baseURLOf(*base), fs.Arg(0))
	req, err := http.NewRequest(http.MethodGet, target, nil)
	if err != nil {
		fmt.Fprintf(stderr, "goclipboard pull: %v\n", err)
		return ExitError
	}
	if p := strings.TrimSpace(*password); p != "" {
		req.Header.Set("X-Goclip-Password", p)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		fmt.Fprintf(stderr, "goclipboard pull: %v\n", err)
		return ExitError
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusUnauthorized {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		fmt.Fprintf(stderr, "goclipboard pull: room is password-protected: %s (use -password)\n", strings.TrimSpace(string(msg)))
		return ExitError
	}
	if resp.StatusCode != http.StatusOK {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		fmt.Fprintf(stderr, "goclipboard pull: server %d: %s\n", resp.StatusCode, strings.TrimSpace(string(msg)))
		return ExitError
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, MaxPushBytes+1))
	if err != nil {
		fmt.Fprintf(stderr, "goclipboard pull: %v\n", err)
		return ExitError
	}

	// The API returns a JSON envelope; print just the content field so pull is
	// pipe-friendly. Non-JSON bodies (weird proxies) pass through verbatim.
	content := string(body)
	var envelope struct {
		Content string `json:"content"`
	}
	if json.Unmarshal(body, &envelope) == nil {
		content = envelope.Content
	}

	var out io.Writer = stdout
	var file *os.File
	if *outFile != "" {
		f, err := os.Create(*outFile)
		if err != nil {
			fmt.Fprintf(stderr, "goclipboard pull: %v\n", err)
			return ExitError
		}
		out = f
		file = f
	}
	if _, err := io.WriteString(out, content); err != nil {
		fmt.Fprintf(stderr, "goclipboard pull: %v\n", err)
		return ExitError
	}
	if file != nil {
		if err := file.Close(); err != nil {
			fmt.Fprintf(stderr, "goclipboard pull: %v\n", err)
			return ExitError
		}
	}
	return ExitOK
}

// ---------------------------------------------------------------------------

func baseURLOf(flagValue string) string {
	if flagValue != "" {
		return strings.TrimRight(flagValue, "/")
	}
	if env := strings.TrimSpace(os.Getenv("GOCLIPBOARD_URL")); env != "" {
		return strings.TrimRight(env, "/")
	}
	return DefaultBaseURL
}

// resolveTarget turns a bare room key or a full URL into a GET-able API URL.
// Page URLs (https://host/{key}) and view URLs (…?view=…) are translated to
// the API path; direct /api/clipboard/{key} URLs pass through unchanged.
func resolveTarget(baseURL, ref string) string {
	ref = strings.TrimSpace(ref)
	if strings.HasPrefix(ref, "http://") || strings.HasPrefix(ref, "https://") {
		if u, err := url.Parse(ref); err == nil {
			p := strings.Trim(u.Path, "/")
			if strings.HasPrefix(p, "api/clipboard/") {
				return ref // already an API URL
			}
			if p != "" && !strings.Contains(p, "/") {
				return u.Scheme + "://" + u.Host + "/api/clipboard/" + p
			}
		}
		return ref
	}
	key := strings.Trim(ref, "/")
	// Strip scheme-less host form "host/key".
	if i := strings.Index(key, "/"); i >= 0 && strings.Contains(key[:i], ".") {
		key = key[i+1:]
	}
	return baseURL + "/api/clipboard/" + key
}

func readInput(stdin io.Reader, file string) (string, error) {
	var r io.Reader = stdin
	if file != "" {
		f, err := os.Open(file)
		if err != nil {
			return "", fmt.Errorf("open %s: %w", file, err)
		}
		defer f.Close()
		r = f
	}
	data, err := io.ReadAll(io.LimitReader(r, MaxPushBytes+1))
	if err != nil {
		return "", err
	}
	if len(data) > MaxPushBytes {
		return "", errors.New("content exceeds 1 MiB server limit")
	}
	return string(data), nil
}

func parseTTL(s string) (int64, error) {
	s = strings.TrimSpace(s)
	if n, err := strconv.ParseInt(s, 10, 64); err == nil {
		if n <= 0 {
			return 0, errors.New("ttl must be positive")
		}
		return n, nil
	}
	// "2d" → 48h (time.ParseDuration has no day unit).
	if strings.HasSuffix(s, "d") {
		if h, err := strconv.ParseInt(strings.TrimSuffix(s, "d"), 10, 64); err == nil {
			return h * 86400, nil
		}
	}
	d, err := time.ParseDuration(s)
	if err != nil {
		return 0, err
	}
	if d <= 0 {
		return 0, errors.New("ttl must be positive")
	}
	return int64(d / time.Second), nil
}

func doJSON(method, url string, body []byte) (int, []byte, error) {
	req, err := http.NewRequest(method, url, bytes.NewReader(body))
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return 0, nil, err
	}
	return resp.StatusCode, data, nil
}
