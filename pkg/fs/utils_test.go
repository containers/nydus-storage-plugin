package fs

import (
	"testing"

	"github.com/hanwen/go-fuse/v2/fuse"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
)

func TestUtilsCopyAttr(t *testing.T) {
	var src, dst fuse.Attr
	src.Ino = 123
	src.Size = 456
	src.Mode = 0o644
	src.Nlink = 2
	src.Owner = fuse.Owner{Uid: 1, Gid: 2}
	copyAttr(&dst, &src)
	if dst != src {
		t.Fatalf("attr not copied: %+v != %+v", dst, src)
	}
}

func TestUtilsLayerToAttr(t *testing.T) {
	var out fuse.Attr
	s := layerToAttr(&ocispec.Descriptor{Size: 8192}, &out)
	if out.Mode != (fuse.S_IFREG | layerFileMode) {
		t.Fatalf("unexpected mode: %o", out.Mode)
	}
	if s.Mode != fuse.S_IFREG {
		t.Fatalf("stable mode mismatch: got %o", s.Mode)
	}
}

func TestUtilsDefaultFileDirLinkAttr(t *testing.T) {
	var out fuse.Attr
	s1 := defaultFileAttr(100, &out)
	if out.Mode != (fuse.S_IFREG | defaultFileMode) || s1.Mode != fuse.S_IFREG {
		t.Fatalf("defaultFileAttr unexpected: out.Mode=%o stable.Mode=%o", out.Mode, s1.Mode)
	}
	s2 := defaultDirAttr(&out)
	if out.Mode != (fuse.S_IFDIR | defaultDirMode) || s2.Mode != fuse.S_IFDIR {
		t.Fatalf("defaultDirAttr unexpected: out.Mode=%o stable.Mode=%o", out.Mode, s2.Mode)
	}
	s3 := defaultLinkAttr(&out)
	if out.Mode != (fuse.S_IFLNK | defaultLinkMode) || s3.Mode != fuse.S_IFLNK {
		t.Fatalf("defaultLinkAttr unexpected: out.Mode=%o stable.Mode=%o", out.Mode, s3.Mode)
	}
}
