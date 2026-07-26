package crdt

import (
	"fmt"
	"strings"
)

// Item is one character atom in the RGA tree (may be a tombstone).
type Item struct {
	ID      string `json:"id"`
	After   string `json:"after"`
	Value   string `json:"ch"`
	Deleted bool   `json:"del"`
}

// Doc is an insert-after RGA sequence CRDT.
// Each item's After is its parent; children of a parent are ordered by ID ascending.
// Document order is DFS pre-order from the root parent "".
type Doc struct {
	items    map[string]*Item
	children map[string][]string // parent id -> sorted child ids
}

// NewDoc returns an empty document.
func NewDoc() *Doc {
	return &Doc{
		items:    make(map[string]*Item),
		children: make(map[string][]string),
	}
}

// Len returns total items including tombstones.
func (d *Doc) Len() int {
	return len(d.items)
}

// Insert integrates an insert op. Idempotent if id already exists.
// Parent (after) must exist unless after is "" (document head).
// Returns whether the document changed.
func (d *Doc) Insert(id, after, ch string) (bool, error) {
	if id == "" {
		return false, fmt.Errorf("empty insert id")
	}
	if _, _, err := ParseID(id); err != nil {
		return false, err
	}
	if !SingleCodePoint(ch) {
		return false, fmt.Errorf("ch must be a single code point")
	}
	if _, exists := d.items[id]; exists {
		return false, nil
	}
	if after != "" {
		if _, ok := d.items[after]; !ok {
			return false, fmt.Errorf("unknown parent %q", after)
		}
	}
	item := &Item{ID: id, After: after, Value: ch, Deleted: false}
	d.items[id] = item
	d.insertChild(after, id)
	return true, nil
}

// Delete marks id as a tombstone. Missing or already-deleted ids are no-ops.
// Returns whether the document changed.
func (d *Doc) Delete(id string) bool {
	item, ok := d.items[id]
	if !ok || item.Deleted {
		return false
	}
	item.Deleted = true
	return true
}

func (d *Doc) insertChild(parent, id string) {
	kids := d.children[parent]
	// Sibling order: higher Lamport clock first (see CompareSibling).
	i := 0
	for i < len(kids) && CompareSibling(kids[i], id) < 0 {
		i++
	}
	if i < len(kids) && kids[i] == id {
		return
	}
	kids = append(kids, "")
	copy(kids[i+1:], kids[i:])
	kids[i] = id
	d.children[parent] = kids
}

// MaxClock returns the highest Lamport clock present in the document.
func (d *Doc) MaxClock() int64 {
	var max int64
	for id := range d.items {
		if c := MaxClock(id); c > max {
			max = c
		}
	}
	return max
}

// Materialize returns the visible document string (skipping tombstones).
func (d *Doc) Materialize() string {
	var b strings.Builder
	d.walk("", func(item *Item) {
		if !item.Deleted {
			b.WriteString(item.Value)
		}
	})
	return b.String()
}

// VisibleIDs returns item ids for live characters in document order.
func (d *Doc) VisibleIDs() []string {
	var ids []string
	d.walk("", func(item *Item) {
		if !item.Deleted {
			ids = append(ids, item.ID)
		}
	})
	return ids
}

// Items returns a snapshot of all items in DFS order (including tombstones).
func (d *Doc) Items() []Item {
	out := make([]Item, 0, len(d.items))
	d.walk("", func(item *Item) {
		out = append(out, *item)
	})
	return out
}

// FromItems rebuilds a document from a snapshot. Items may be in any order;
// parents must be present (empty After is the root).
func (d *Doc) FromItems(items []Item) error {
	d.items = make(map[string]*Item, len(items))
	d.children = make(map[string][]string)
	// First register all items so parent checks can pass in any order.
	for i := range items {
		it := items[i]
		if it.ID == "" {
			return fmt.Errorf("empty item id")
		}
		if _, _, err := ParseID(it.ID); err != nil {
			return err
		}
		if !SingleCodePoint(it.Value) {
			return fmt.Errorf("item %q: ch must be a single code point", it.ID)
		}
		if _, exists := d.items[it.ID]; exists {
			return fmt.Errorf("duplicate item id %q", it.ID)
		}
		cp := it
		d.items[it.ID] = &cp
	}
	for _, it := range items {
		if it.After != "" {
			if _, ok := d.items[it.After]; !ok {
				return fmt.Errorf("item %q: unknown parent %q", it.ID, it.After)
			}
		}
		d.insertChild(it.After, it.ID)
	}
	return nil
}

// Clone returns a deep copy.
func (d *Doc) Clone() *Doc {
	out := NewDoc()
	for id, item := range d.items {
		cp := *item
		out.items[id] = &cp
	}
	for parent, kids := range d.children {
		cp := make([]string, len(kids))
		copy(cp, kids)
		out.children[parent] = cp
	}
	return out
}

// BuildFromString creates a linear chain of inserts under site with clocks 1..n.
func BuildFromString(site, content string) (*Doc, error) {
	if site == "" {
		return nil, fmt.Errorf("empty site")
	}
	d := NewDoc()
	if content == "" {
		return d, nil
	}
	prev := ""
	var clock int64
	for _, r := range content {
		clock++
		id := FormatID(site, clock)
		if _, err := d.Insert(id, prev, string(r)); err != nil {
			return nil, err
		}
		prev = id
	}
	return d, nil
}

func (d *Doc) walk(parent string, fn func(*Item)) {
	for _, cid := range d.children[parent] {
		item := d.items[cid]
		if item == nil {
			continue
		}
		fn(item)
		d.walk(cid, fn)
	}
}
