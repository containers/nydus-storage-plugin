package fs

import (
	"testing"
)

type tr struct{ releasableFlag bool }

func (r *tr) releasable() bool { return r.releasableFlag }

func TestIDMapAddAssignsAndReuses(t *testing.T) {
	m := &idMap{}
	var saved []*tr
	add := func(_ uint32) (releasable, error) {
		r := &tr{}
		saved = append(saved, r)
		return r, nil
	}
	if err := m.add(add); err != nil {
		t.Fatalf("add #1: %v", err)
	}
	if err := m.add(add); err != nil {
		t.Fatalf("add #2: %v", err)
	}
	// IDs should start from 2 (skipping 0 and 1), so expect two entries
	if len(m.m) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(m.m))
	}
	// Mark first releasable and add again, expecting reuse of freed ID
	saved[0].releasableFlag = true
	if err := m.add(add); err != nil {
		t.Fatalf("add #3: %v", err)
	}
	if len(m.m) != 2 {
		t.Fatalf("expected still 2 entries after reuse, got %d", len(m.m))
	}
}
