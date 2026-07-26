package crdt

import (
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"
)

// FormatID builds "site:clock".
func FormatID(site string, clock int64) string {
	return site + ":" + strconv.FormatInt(clock, 10)
}

// ParseID splits "site:clock". Site must be non-empty and must not contain ':'.
func ParseID(id string) (site string, clock int64, err error) {
	if id == "" {
		return "", 0, fmt.Errorf("empty id")
	}
	i := strings.IndexByte(id, ':')
	if i <= 0 || i == len(id)-1 {
		return "", 0, fmt.Errorf("invalid id %q", id)
	}
	site = id[:i]
	clock, err = strconv.ParseInt(id[i+1:], 10, 64)
	if err != nil || clock <= 0 {
		return "", 0, fmt.Errorf("invalid id clock %q", id)
	}
	if strings.ContainsRune(site, ':') {
		return "", 0, fmt.Errorf("invalid id site %q", id)
	}
	return site, clock, nil
}

// CompareID orders by site ascending, then clock ascending.
// Returns -1 if a < b, 0 if equal, 1 if a > b.
func CompareID(a, b string) int {
	if a == b {
		return 0
	}
	as, ac, aerr := ParseID(a)
	bs, bc, berr := ParseID(b)
	if aerr != nil || berr != nil {
		// Fall back to plain string order for robustness.
		if a < b {
			return -1
		}
		if a > b {
			return 1
		}
		return 0
	}
	if as < bs {
		return -1
	}
	if as > bs {
		return 1
	}
	if ac < bc {
		return -1
	}
	if ac > bc {
		return 1
	}
	return 0
}

// CompareSibling orders children of the same parent for document DFS.
// Newer Lamport clocks come first (immediately after the parent); equal clocks
// break ties by site ascending. Returns -1 if a should appear before b.
//
// Clients MUST allocate clocks as Lamport timestamps (strictly greater than any
// clock already observed in the document). Otherwise mid-string inserts land
// after an entire sibling subtree (characters "jump" to the wrong line).
func CompareSibling(a, b string) int {
	if a == b {
		return 0
	}
	as, ac, aerr := ParseID(a)
	bs, bc, berr := ParseID(b)
	if aerr != nil || berr != nil {
		return CompareID(a, b)
	}
	// Higher clock first.
	if ac > bc {
		return -1
	}
	if ac < bc {
		return 1
	}
	// Same clock: site ascending for deterministic concurrent merges.
	if as < bs {
		return -1
	}
	if as > bs {
		return 1
	}
	return 0
}

// MaxClock returns the maximum clock component among ids (0 if none parse).
func MaxClock(ids ...string) int64 {
	var max int64
	for _, id := range ids {
		_, c, err := ParseID(id)
		if err == nil && c > max {
			max = c
		}
	}
	return max
}

// SingleCodePoint reports whether s is exactly one Unicode code point.
func SingleCodePoint(s string) bool {
	if s == "" {
		return false
	}
	_, size := utf8.DecodeRuneInString(s)
	return size == len(s) && size > 0
}
