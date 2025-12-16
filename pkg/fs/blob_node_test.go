package fs

import (
	"context"
	"testing"
)

func TestBlobNodeOpenWithoutInitReturnsError(t *testing.T) {
	n := &blobNode{}
	_, _, eno := n.Open(context.Background(), 0)
	if eno == 0 {
		t.Fatalf("expected non-zero errno for uninitialized blobNode")
	}
}
