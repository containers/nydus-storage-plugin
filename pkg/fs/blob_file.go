package fs

import (
	"context"
	"io"
	"log/slog"
	"sync"
	"syscall"

	"github.com/containerd/containerd/remotes/docker"
	fusefs "github.com/hanwen/go-fuse/v2/fs"
	"github.com/hanwen/go-fuse/v2/fuse"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
)

// blobFile streams a remote blob and supports random access by reopening
// the stream and skipping to the requested offset when needed.
type blobFile struct {
	mu      sync.Mutex
	rc      io.ReadCloser
	size    int64
	pos     int64
	ref     string
	desc    ocispec.Descriptor
	hostsFn func(string) ([]docker.RegistryHost, error)
}

func newBlobFile(_ context.Context, ref string, desc ocispec.Descriptor, hostsFn func(string) ([]docker.RegistryHost, error)) (*blobFile, error) {
	return &blobFile{
		size:    desc.Size,
		pos:     0,
		ref:     ref,
		desc:    desc,
		hostsFn: hostsFn,
	}, nil
}

var _ = (fusefs.FileReader)((*blobFile)(nil))
var _ = (fusefs.FileReleaser)((*blobFile)(nil))
var _ = (fusefs.FileGetattrer)((*blobFile)(nil))

// openAt (re)opens the remote stream and skips to the specified offset.
func (f *blobFile) openAt(ctx context.Context, off int64) error {
	if f.rc != nil {
		_ = f.rc.Close()
		f.rc = nil
	}
	resolver := docker.NewResolver(docker.ResolverOptions{
		Hosts: func(host string) ([]docker.RegistryHost, error) { return f.hostsFn(host) },
	})
	fetcher, err := resolver.Fetcher(ctx, f.ref)
	if err != nil {
		return err
	}
	r, err := fetcher.Fetch(ctx, f.desc)
	if err != nil {
		return err
	}
	// Skip to desired offset (naive sequential skip). Can be optimized with HTTP Range later.
	if off > 0 {
		if _, err := io.CopyN(io.Discard, r, off); err != nil {
			_ = r.Close()
			return err
		}
	}
	f.rc = r
	f.pos = off
	return nil
}

func (f *blobFile) Read(ctx context.Context, dest []byte, off int64) (fuse.ReadResult, syscall.Errno) {
	f.mu.Lock()
	defer f.mu.Unlock()

	// Guard against invalid handles (primarily for tests) and avoid panics.
	if f.hostsFn == nil || f.ref == "" {
		return nil, syscall.EIO
	}

	if f.rc == nil || off != f.pos {
		if err := f.openAt(ctx, off); err != nil {
			slog.Warn("blob openAt failed", "off", off, "err", err)
			return nil, syscall.EIO
		}
	}
	if len(dest) == 0 {
		return fuse.ReadResultData(nil), 0
	}
	n, err := f.rc.Read(dest)
	if n > 0 {
		f.pos += int64(n)
	}
	if err != nil && err != io.EOF {
		return nil, syscall.EIO
	}
	return fuse.ReadResultData(dest[:n]), 0
}

func (f *blobFile) Release(_ context.Context) syscall.Errno {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.rc != nil {
		_ = f.rc.Close()
		f.rc = nil
	}
	return 0
}

func (f *blobFile) Getattr(_ context.Context, out *fuse.AttrOut) syscall.Errno {
	out.Size = uint64(f.size)
	return 0
}
