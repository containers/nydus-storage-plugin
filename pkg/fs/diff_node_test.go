package fs

import (
	"context"
	"testing"

	"github.com/hanwen/go-fuse/v2/fuse"
)

func TestDiffNodeGetattrCopiesAttr(t *testing.T) {
	d := &diffNode{}
	d.attr.Mode = 0o755
	var out fuse.AttrOut
	if eno := d.Getattr(context.Background(), nil, &out); eno != 0 {
		t.Fatalf("Getattr errno=%d", eno)
	}
	if out.Mode != d.attr.Mode {
		t.Fatalf("mode mismatch: got %o want %o", out.Mode, d.attr.Mode)
	}
}

func TestDiffNodeRmdirAlwaysENOENT(t *testing.T) {
	d := &diffNode{}
	if eno := d.Rmdir(context.Background(), "anything"); eno == 0 {
		t.Fatalf("expected ENOENT, got 0")
	}
}
