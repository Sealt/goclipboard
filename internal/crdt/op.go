package crdt

import (
	"fmt"
)

const (
	OpInsert = "ins"
	OpDelete = "del"

	// MaxOpsPerBatch caps a single apply batch.
	MaxOpsPerBatch = 4096
	// MaxItems soft cap on document size including tombstones.
	MaxItems = 2_000_000
)

// Op is a single CRDT operation.
type Op struct {
	Op    string `json:"op"`
	ID    string `json:"id"`
	After string `json:"after,omitempty"`
	Ch    string `json:"ch,omitempty"`
}

// ValidateOp checks structural constraints (not document causality).
func ValidateOp(op Op) error {
	switch op.Op {
	case OpInsert:
		if _, _, err := ParseID(op.ID); err != nil {
			return fmt.Errorf("ins: %w", err)
		}
		if op.After != "" {
			if _, _, err := ParseID(op.After); err != nil {
				return fmt.Errorf("ins after: %w", err)
			}
		}
		if !SingleCodePoint(op.Ch) {
			return fmt.Errorf("ins: ch must be a single code point")
		}
		return nil
	case OpDelete:
		if _, _, err := ParseID(op.ID); err != nil {
			return fmt.Errorf("del: %w", err)
		}
		return nil
	default:
		return fmt.Errorf("unknown op %q", op.Op)
	}
}

// ValidateBatch checks batch size and each op.
func ValidateBatch(ops []Op) error {
	if len(ops) == 0 {
		return fmt.Errorf("empty ops")
	}
	if len(ops) > MaxOpsPerBatch {
		return fmt.Errorf("too many ops (max %d)", MaxOpsPerBatch)
	}
	for i, op := range ops {
		if err := ValidateOp(op); err != nil {
			return fmt.Errorf("ops[%d]: %w", i, err)
		}
	}
	return nil
}

// Apply applies a single op to the document. changed is true if state mutated.
func (d *Doc) Apply(op Op) (changed bool, err error) {
	if err := ValidateOp(op); err != nil {
		return false, err
	}
	switch op.Op {
	case OpInsert:
		if d.Len() >= MaxItems {
			if _, exists := d.items[op.ID]; !exists {
				return false, fmt.Errorf("document too large")
			}
		}
		return d.Insert(op.ID, op.After, op.Ch)
	case OpDelete:
		return d.Delete(op.ID), nil
	default:
		return false, fmt.Errorf("unknown op %q", op.Op)
	}
}

// ApplyBatch validates then applies all ops. On error the document may be
// partially updated; callers that need atomicity should Clone first.
// Returns whether any op mutated the document.
func (d *Doc) ApplyBatch(ops []Op) (changed bool, err error) {
	if err := ValidateBatch(ops); err != nil {
		return false, err
	}
	for i, op := range ops {
		c, err := d.Apply(op)
		if err != nil {
			return changed, fmt.Errorf("ops[%d]: %w", i, err)
		}
		if c {
			changed = true
		}
	}
	return changed, nil
}

// ContentByteLen returns the UTF-8 byte length of the materialized string.
func (d *Doc) ContentByteLen() int {
	n := 0
	d.walk("", func(item *Item) {
		if !item.Deleted {
			n += len(item.Value)
		}
	})
	return n
}
