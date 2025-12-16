// Ported from stargz-snapshotter, copyright The stargz-snapshotter Authors.
// https://github.com/containerd/stargz-snapshotter/blob/efc4166e93a22804b90e27c912eff7ecc0a12dfc/store/fs.go#L306-L456
package fs

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"syscall"

	fusefs "github.com/hanwen/go-fuse/v2/fs"
	"github.com/hanwen/go-fuse/v2/fuse"
	"github.com/opencontainers/go-digest"
)

// layernode is the node at <mountpoint>/<imageref>/<layerdigest>.
type layerNode struct {
	fusefs.Inode
	attr fuse.Attr
	fs   *fs

	refNode *refNode
	digest  digest.Digest
}

var _ = (fusefs.InodeEmbedder)((*layerNode)(nil))
var _ = (fusefs.NodeCreater)((*layerNode)(nil))
var _ = (fusefs.NodeLookuper)((*layerNode)(nil))
var _ = (fusefs.NodeReaddirer)((*layerNode)(nil))

// Create marks this layer as "using".
// We don't use refnode.Mkdir because Mkdir event doesn't reach here if layernode already exists.
func (n *layerNode) Create(ctx context.Context, name string, _ uint32, _ uint32, _ *fuse.EntryOut) (node *fusefs.Inode, fh fusefs.FileHandle, fuseFlags uint32, errno syscall.Errno) {
	if name == layerUseFile {
		current := n.fs.layManager.Use(n.refNode.ref, n.digest)
		slog.InfoContext(ctx, "layer marked USING", "ref", n.refNode.ref.String(), "digest", n.digest.String(), "refcounter", current)
	}
	return nil, nil, 0, syscall.ENOENT
}

// Lookup routes to the target file stored in the pool, based on the specified file name.
func (n *layerNode) Lookup(ctx context.Context, name string, out *fuse.EntryOut) (*fusefs.Inode, syscall.Errno) {
	slog.InfoContext(ctx, "layer node lookup", "name", name)
	switch name {
	case layerInfoLink:
		info, err := n.fs.layManager.GetLayerInfo(ctx, n.refNode.ref, n.digest)
		if err != nil {
			slog.WarnContext(ctx, "failed to get layer info", "name", name, "digest", n.digest.String(), "err", err)
			return nil, syscall.EIO
		}
		buf := new(bytes.Buffer)
		if err := json.NewEncoder(buf).Encode(&info); err != nil {
			slog.WarnContext(ctx, "failed to encode layer info", "name", name, "digest", n.digest.String(), "err", err)
			return nil, syscall.EIO
		}
		infoData := buf.Bytes()
		sAttr := defaultFileAttr(uint64(len(infoData)), &out.Attr)
		cn := &fusefs.MemRegularFile{Data: infoData}
		copyAttr(&cn.Attr, &out.Attr)
		return n.fs.newInodeWithID(ctx, func(ino uint32) fusefs.InodeEmbedder {
			out.Ino = uint64(ino)
			cn.Attr.Ino = uint64(ino)
			sAttr.Ino = uint64(ino)
			return n.NewInode(ctx, cn, sAttr)
		})
	case layerLink, blobLink:
		if name == layerLink {
			n.fs.knownNodeMu.Lock()
			if lh, ok := n.fs.knownNode[n.refNode.ref.String()][n.digest.String()]; ok {
				var ao fuse.AttrOut
				if errno := lh.n.(fusefs.NodeGetattrer).Getattr(ctx, nil, &ao); errno != 0 {
					return nil, errno
				}
				copyAttr(&out.Attr, &ao.Attr)
				n.fs.knownNodeMu.Unlock()
				return n.NewInode(ctx, lh.n, fusefs.StableAttr{
					Mode: out.Mode,
					Ino:  out.Ino,
				}), 0
			}
			n.fs.knownNodeMu.Unlock()
		}

		l, err := n.fs.layManager.ResolverMetaLayer(ctx, n.refNode.ref, n.refNode.rawRef, n.digest)
		if err != nil {
			slog.WarnContext(ctx, "resolve meta layer failed", "err", err)
			if name == layerLink {
				return nil, syscall.ENOENT
			}
		}
		if name == blobLink {
			sAttr := layerToAttr(&l.Descriptor, &out.Attr)
			cn := &blobNode{l: &l.Descriptor, fs: n.fs, ref: n.refNode.ref}
			copyAttr(&cn.attr, &out.Attr)
			return n.fs.newInodeWithID(ctx, func(ino uint32) fusefs.InodeEmbedder {
				out.Ino = uint64(ino)
				cn.attr.Ino = uint64(ino)
				sAttr.Ino = uint64(ino)
				return n.NewInode(ctx, cn, sAttr)
			})
		}

		// Only Nydus layers expose a diff directory.
		// - bootstrap layer: diff is backed by a bind mount to the nydusd mountpoint
		// - data blob layers: diff is an intentionally empty directory (to satisfy Podman additional layer store expectations)
		if !l.IsMetaLayer || l.MountFailed {
			slog.InfoContext(ctx, "not a nydus layer; no diff provided", "digest", n.digest.String())
			return nil, syscall.ENOENT
		}

		sAttr := defaultDirAttr(&out.Attr)
		child := &diffNode{
			fs: n.fs,
		}
		copyAttr(&child.attr, &out.Attr)
		return n.fs.newInodeWithID(ctx, func(ino uint32) fusefs.InodeEmbedder {
			out.Ino = uint64(ino)
			child.attr.Ino = uint64(ino)
			sAttr.Ino = uint64(ino)
			cn := n.NewInode(ctx, child, sAttr)

			rr := &layerReleasable{n: child}
			n.fs.knownNodeMu.Lock()
			if n.fs.knownNode == nil {
				n.fs.knownNode = make(map[string]map[string]*layerReleasable)
			}
			if n.fs.knownNode[n.refNode.ref.String()] == nil {
				n.fs.knownNode[n.refNode.ref.String()] = make(map[string]*layerReleasable)
			}
			n.fs.knownNode[n.refNode.ref.String()][n.digest.String()] = rr
			n.fs.knownNodeMu.Unlock()
			return cn
		})
	case layerUseFile:
		slog.InfoContext(ctx, "use file referred; returning ENOENT for reference mgmt")
		return nil, syscall.ENOENT
	default:
		slog.WarnContext(ctx, "unknown filename", "name", name)
		return nil, syscall.ENOENT
	}
}

// Readdir enumerates expected entries to help consumers like Podman discover files reliably.
func (n *layerNode) Readdir(_ context.Context) (fusefs.DirStream, syscall.Errno) {
	entries := []fuse.DirEntry{
		{Name: layerInfoLink, Mode: fuse.S_IFREG},
		{Name: blobLink, Mode: fuse.S_IFREG},
		{Name: layerLink, Mode: fuse.S_IFDIR},
		{Name: layerUseFile, Mode: fuse.S_IFREG},
	}
	return fusefs.NewListDirStream(entries), 0
}
