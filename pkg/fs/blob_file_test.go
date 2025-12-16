package fs

import (
	"context"
	"testing"

	"github.com/hanwen/go-fuse/v2/fuse"
)

func TestBlobFileReadEIO(t *testing.T) {
	bf := &blobFile{}
	if rr, eno := bf.Read(context.Background(), nil, 0); eno == 0 || rr != nil {
		t.Fatalf("expected EIO and nil read result, got eno=%d rr=%v", eno, rr)
	}
}

func TestBlobFileGetattrNoError(t *testing.T) {
	bf := &blobFile{}
	var out fuse.AttrOut
	if eno := bf.Getattr(context.Background(), &out); eno != 0 {
		t.Fatalf("Getattr returned errno=%d", eno)
	}
}
