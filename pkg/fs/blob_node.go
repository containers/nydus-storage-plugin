package fs

import (
	"context"
	"syscall"

	"github.com/containerd/containerd/reference"
	"github.com/containerd/containerd/remotes/docker"
	fusefs "github.com/hanwen/go-fuse/v2/fs"
	"github.com/hanwen/go-fuse/v2/fuse"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
)

// blobNode is a regular file node that contains raw blob data
type blobNode struct {
	fusefs.Inode
	attr fuse.Attr
	l    *ocispec.Descriptor
	fs   *fs
	ref  reference.Spec
}

var _ = (fusefs.InodeEmbedder)((*blobNode)(nil))

var _ = (fusefs.NodeOpener)((*blobNode)(nil))

func (n *blobNode) Open(ctx context.Context, _ uint32) (fh fusefs.FileHandle, fuseFlags uint32, errno syscall.Errno) {
	if n == nil || n.fs == nil || n.fs.layManager == nil || n.l == nil {
		return nil, 0, syscall.EIO
	}
	hostsFn := func(_ string) ([]docker.RegistryHost, error) {
		return n.fs.layManager.Hosts()(n.ref)
	}
	bf, err := newBlobFile(ctx, n.ref.String(), *n.l, hostsFn)
	if err != nil {
		return nil, 0, syscall.EIO
	}
	return bf, 0, 0
}
