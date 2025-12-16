package fs

import (
	"context"
	"testing"

	"github.com/hanwen/go-fuse/v2/fuse"
)

func TestLayerNodeLookupUnknownNameENOENT(t *testing.T) {
	n := &layerNode{fs: &fs{}}
	var out fuse.EntryOut
	_, eno := n.Lookup(context.Background(), "unknown", &out)
	if eno == 0 {
		t.Fatalf("expected ENOENT for unknown file name")
	}
}
