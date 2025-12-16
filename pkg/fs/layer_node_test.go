package fs

import (
	"context"
	"testing"

	"github.com/hanwen/go-fuse/v2/fuse"
)

func TestLayerNodeReaddir(t *testing.T) {
	n := &layerNode{}
	ds, errno := n.Readdir(context.Background())
	if errno != 0 {
		t.Fatalf("Readdir returned errno=%d", errno)
	}

	var names []string
	var modes []uint32
	for ds.HasNext() {
		de, _ := ds.Next()
		names = append(names, de.Name)
		modes = append(modes, de.Mode)
	}

	expectNames := []string{layerInfoLink, blobLink, layerLink, layerUseFile}
	if len(names) != len(expectNames) {
		t.Fatalf("unexpected entries len: got %d, want %d", len(names), len(expectNames))
	}
	for i, want := range expectNames {
		if names[i] != want {
			t.Errorf("entry %d name mismatch: got %q want %q", i, names[i], want)
		}
	}
	// modes
	expectModes := []uint32{fuse.S_IFREG, fuse.S_IFREG, fuse.S_IFDIR, fuse.S_IFREG}
	for i, want := range expectModes {
		if modes[i] != want {
			t.Errorf("entry %d mode mismatch: got %v want %v", i, modes[i], want)
		}
	}
}
