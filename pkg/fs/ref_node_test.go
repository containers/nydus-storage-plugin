package fs

import (
	"context"
	"testing"
)

func TestRefNodeRmdirInvalidDigest(t *testing.T) {
	ref := refNode{fs: &fs{}}
	if eno := ref.Rmdir(context.Background(), "not-a-digest"); eno == 0 {
		t.Fatalf("expected error for invalid digest")
	}
}
