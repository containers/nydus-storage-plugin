// Ported from stargz-snapshotter, copyright The stargz-snapshotter Authors.
// https://github.com/containerd/stargz-snapshotter/blob/efc4166e93a22804b90e27c912eff7ecc0a12dfc/store/fs.go#L43-#L157
package fs

import (
	"context"
	"os/exec"
	"sync"
	"syscall"
	"time"

	"log/slog"

	"github.com/containers/nydus-storage-plugin/pkg/manager"
	fusefs "github.com/hanwen/go-fuse/v2/fs"
	"github.com/hanwen/go-fuse/v2/fuse"
)

const (
	blockSize = 4096

	poolLink      = "pool"
	layerLink     = "diff"
	blobLink      = "blob"
	layerInfoLink = "info"
	layerUseFile  = "use"
)

// Configurable FS modes (defaults preserved)
var (
	// Permission bits only (no file type bits). File type is set via fuse StableAttr.
	defaultLinkMode uint32 = 0400 // -r-------- (symlink perms are largely ignored by kernels)
	defaultDirMode  uint32 = 0500 // dr-x------
	defaultFileMode uint32 = 0400 // -r--------
	layerFileMode   uint32 = 0400 // -r--------

	// Mount behavior toggles
	defaultAllowOther bool = true
	forceDirectMount  bool = false
	enableSuidOption  bool = false
)

// Helpers to expose defaults for CLI parsing
func DefaultFileMode() uint32 { return defaultFileMode }
func DefaultDirMode() uint32  { return defaultDirMode }
func DefaultLinkMode() uint32 { return defaultLinkMode }

// Mount options
type MountOption func()

// WithModes overrides default FS modes at mount time.
func WithModes(fileMode, dirMode, linkMode uint32) MountOption {
	return func() {
		defaultFileMode = fileMode
		defaultDirMode = dirMode
		defaultLinkMode = linkMode
		layerFileMode = fileMode
	}
}

// WithAllowOther toggles the allow_other mount option.
func WithAllowOther(b bool) MountOption {
	return func() {
		defaultAllowOther = b
	}
}

// WithDirectMount forces direct mount, bypassing fusermount helpers.
func WithDirectMount(b bool) MountOption {
	return func() {
		forceDirectMount = b
	}
}

// WithMountSuid toggles "suid" mount option when using fusermount.
func WithMountSuid(b bool) MountOption {
	return func() { enableSuidOption = b }
}

type releasable interface {
	releasable() bool
}

type fs struct {
	// nodeMap manages inode numbers for nodes other than nodes in layers
	// (i.e. nodes other than ones inside `diff` directories).
	// - inode number = [ 0 ][ uint32 ID ]
	nodeMap *idMap
	// layerMap manages upper bits of inode numbers for nodes inside layers.
	// - inode number = [ uint32 layer ID ][ uint32 number (unique inside `diff` directory) ]
	// inodes numbers of noeds inside each `diff` directory are prefixed by an unique uint32
	// so that they don't conflict with nodes outside `diff` directories.
	layerMap *idMap

	knownNode   map[string]map[string]*layerReleasable
	knownNodeMu sync.Mutex
	layManager  *manager.LayerManager
}

type layerReleasable struct {
	n        fusefs.InodeEmbedder
	released bool
	mu       sync.Mutex
}

func (lh *layerReleasable) releasable() bool {
	lh.mu.Lock()
	released := lh.released
	lh.mu.Unlock()
	return released && isForgotten(lh.n.EmbeddedInode())
}

func (lh *layerReleasable) release() {
	lh.mu.Lock()
	lh.released = true
	lh.mu.Unlock()
}

type inoReleasable struct {
	n fusefs.InodeEmbedder
}

func (r *inoReleasable) releasable() bool {
	return r.n.EmbeddedInode().Forgotten()
}

func Mount(_ context.Context, mountPoint string, _ string, debug bool, layManager *manager.LayerManager, opts ...MountOption) error {
	// Apply mount options
	for _, o := range opts {
		if o != nil {
			o()
		}
	}

	seconds := time.Second
	rawFS := fusefs.NewNodeFS(&rootNode{
		fs: &fs{
			nodeMap:    new(idMap),
			layerMap:   new(idMap),
			layManager: layManager,
		},
	}, &fusefs.Options{
		AttrTimeout:     &seconds,
		EntryTimeout:    &seconds,
		NullPermissions: true,
	})
	mountOpts := &fuse.MountOptions{
		AllowOther: defaultAllowOther, // allow users other than root&mounter to access fs
		FsName:     "nydusstore",
		Debug:      debug,
	}
	// Detect fusermount or fusermount3; fallback to direct mount if neither present
	if hasFusermount() && !forceDirectMount {
		if enableSuidOption {
			mountOpts.Options = append(mountOpts.Options, "suid") // optional
		}
		slog.Info("using fusermount helper", "allowOther", mountOpts.AllowOther)
	} else {
		if !hasFusermount() {
			slog.Info("fusermount/fusermount3 not installed; trying direct mount")
		}
		if forceDirectMount {
			slog.Info("forcing direct mount per option")
		}
		mountOpts.DirectMount = true
	}
	server, err := fuse.NewServer(rawFS, mountPoint, mountOpts)
	if err != nil {
		return err
	}
	go server.Serve()
	return server.WaitMount()
}

func hasFusermount() bool {
	if _, err := exec.LookPath("fusermount"); err == nil {
		return true
	}
	if _, err := exec.LookPath("fusermount3"); err == nil {
		return true
	}
	return false
}

func (fs *fs) newInodeWithID(ctx context.Context, p func(uint32) fusefs.InodeEmbedder) (*fusefs.Inode, syscall.Errno) {
	var ino fusefs.InodeEmbedder
	if err := fs.nodeMap.add(func(id uint32) (releasable, error) {
		ino = p(id)
		return &inoReleasable{ino}, nil
	}); err != nil || ino == nil {
		slog.DebugContext(ctx, "cannot generate ID", "err", err)
		return nil, syscall.EIO
	}
	return ino.EmbeddedInode(), 0
}
