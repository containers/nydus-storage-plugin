package manager

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/containerd/containerd/reference"
	"github.com/containerd/containerd/snapshots/storage"
	"github.com/containerd/nydus-snapshotter/pkg/label"
	"github.com/opencontainers/go-digest"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
)

type fakeFS struct {
	root string
	mp   string
}

func (f *fakeFS) UpperPath(id string) string { return filepath.Join(f.root, "snapshots", id) }
func (f *fakeFS) PrepareMetaLayer(_ context.Context, _ storage.Snapshot, _ map[string]string) error {
	return nil
}
func (f *fakeFS) Mount(_ context.Context, _ string, _ map[string]string) error {
	return nil
}
func (f *fakeFS) WaitUntilReady(_ context.Context, _ string) error { return nil }
func (f *fakeFS) MountPoint(_ string) (string, error)              { return f.mp, nil }

func TestResolverMetaLayerBindMountAndRemountRO(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()

	// prepare manifest+config in refPool
	p, err := newRefPool(ctx, dir, nil)
	if err != nil {
		t.Fatalf("newRefPool: %v", err)
	}
	refspec, _ := reference.Parse("docker.io/library/busybox:latest")
	dgst := digest.FromString("layer-1")
	manifest := ocispec.Manifest{Layers: []ocispec.Descriptor{{
		MediaType:   ocispec.MediaTypeImageLayerGzip,
		Digest:      dgst,
		Size:        10,
		Annotations: map[string]string{label.NydusMetaLayer: "true"},
	}}, Config: ocispec.Descriptor{MediaType: ocispec.MediaTypeImageConfig, Digest: digest.FromString("cfg"), Size: 1}}
	config := ocispec.Image{RootFS: ocispec.RootFS{Type: "layers", DiffIDs: []digest.Digest{digest.FromString("diffid")}}}
	if err := p.writeManifestAndConfig(refspec, manifest, config); err != nil {
		t.Fatalf("writeManifestAndConfig: %v", err)
	}

	mountPoint := filepath.Join(dir, "nydus-mp")
	if err := os.MkdirAll(mountPoint, 0o755); err != nil {
		t.Fatalf("mkdir mountpoint: %v", err)
	}

	lm := &LayerManager{
		refPool:    p,
		refCounter: make(map[string]map[string]int),
		nydusFs:    &fakeFS{root: dir, mp: mountPoint},
		rootDir:    dir,
	}

	// stub unix mount
	origMount := unixMount
	defer func() { unixMount = origMount }()
	type mcall struct {
		src, tgt, fstype, data string
		flags                  uintptr
	}
	var calls []mcall
	unixMount = func(source, target, fstype string, flags uintptr, data string) error {
		calls = append(calls, mcall{source, target, fstype, data, flags})
		return nil
	}

	snapshotID := "snap-1"
	layer, err := lm.ResolverMetaLayer(ctx, refspec, snapshotID, dgst)
	if err != nil {
		t.Fatalf("ResolverMetaLayer error: %v", err)
	}
	if !layer.IsMetaLayer {
		t.Fatalf("expected IsMetaLayer=true")
	}

	targetPath := filepath.Join(dir, "store", snapshotID, dgst.String(), "diff")
	if _, err := os.Stat(targetPath); err != nil {
		t.Fatalf("targetPath not created: %v", err)
	}

	if len(calls) != 2 {
		t.Fatalf("expected 2 mount calls, got %d", len(calls))
	}
	if calls[0].src != mountPoint || calls[0].tgt != targetPath {
		t.Errorf("bind mount args mismatch: got (%q,%q), want (%q,%q)", calls[0].src, calls[0].tgt, mountPoint, targetPath)
	}
	if calls[1].src != "" || calls[1].tgt != targetPath {
		t.Errorf("remount args mismatch: got (%q,%q)", calls[1].src, calls[1].tgt)
	}
	// flags
	if calls[0].flags&(0x1000|0x4000) == 0 { // MS_BIND|MS_REC
		t.Errorf("bind mount flags missing MS_BIND|MS_REC: %#x", calls[0].flags)
	}
	if calls[1].flags&(0x1000|0x20|0x1) == 0 { // MS_BIND|MS_REMOUNT|MS_RDONLY
		t.Errorf("remount flags missing: %#x", calls[1].flags)
	}
}

func TestResolverMetaLayerCreatesTargetDir(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	p, err := newRefPool(ctx, dir, nil)
	if err != nil {
		t.Fatalf("newRefPool: %v", err)
	}
	refspec, _ := reference.Parse("docker.io/library/busybox:latest")
	dgst := digest.FromString("layer-2")
	manifest := ocispec.Manifest{Layers: []ocispec.Descriptor{{
		MediaType:   ocispec.MediaTypeImageLayerGzip,
		Digest:      dgst,
		Size:        10,
		Annotations: map[string]string{label.NydusMetaLayer: "true"},
	}}, Config: ocispec.Descriptor{MediaType: ocispec.MediaTypeImageConfig, Digest: digest.FromString("cfg2"), Size: 1}}
	config := ocispec.Image{RootFS: ocispec.RootFS{Type: "layers", DiffIDs: []digest.Digest{digest.FromString("diffid2")}}}
	if err := p.writeManifestAndConfig(refspec, manifest, config); err != nil {
		t.Fatalf("writeManifestAndConfig: %v", err)
	}
	mountPoint := filepath.Join(dir, "nydus-mp2")
	if err := os.MkdirAll(mountPoint, 0o755); err != nil {
		t.Fatalf("mkdir mountpoint: %v", err)
	}
	lm := &LayerManager{refPool: p, refCounter: make(map[string]map[string]int), nydusFs: &fakeFS{root: dir, mp: mountPoint}, rootDir: dir}
	origMount := unixMount
	defer func() { unixMount = origMount }()
	unixMount = func(_, _, _ string, _ uintptr, _ string) error { return nil }
	snapshotID := "snap-2"
	_, err = lm.ResolverMetaLayer(ctx, refspec, snapshotID, dgst)
	if err != nil {
		t.Fatalf("ResolverMetaLayer error: %v", err)
	}
	targetPath := filepath.Join(dir, "store", snapshotID, dgst.String(), "diff")
	if st, err := os.Stat(targetPath); err != nil || !st.IsDir() {
		t.Fatalf("targetPath not created as dir: %v, st=%v", err, st)
	}
}

func TestReleaseDecrementsAndUnmountsAndCleansMaps(t *testing.T) {
	ctx := context.Background()
	lm := &LayerManager{
		refPool:    &refPool{refcounter: map[string]*releaser{}},
		refCounter: map[string]map[string]int{},
	}
	refspec, _ := reference.Parse("docker.io/library/busybox:latest")
	dgst := digest.FromString("layer-1")
	snapshotID := "snap-1"
	// prepare counters
	lm.refCounter[refspec.String()] = map[string]int{dgst.String(): 1}
	lm.nydusMetaLayer.Store(snapshotID+":"+dgst.String(), "/fake/target")
	lm.refPool.refcounter[refspec.String()] = &releaser{count: 1, release: func() {}}

	var unmounted []string
	origUnmount := unixUnmount
	defer func() { unixUnmount = origUnmount }()
	unixUnmount = func(target string, _ int) error {
		unmounted = append(unmounted, target)
		return nil
	}

	i, err := lm.Release(ctx, refspec, dgst, snapshotID)
	if err != nil {
		t.Fatalf("Release error: %v", err)
	}
	if i != 0 {
		t.Fatalf("expected return 0, got %d", i)
	}
	if !reflect.DeepEqual(unmounted, []string{"/fake/target"}) {
		t.Errorf("unexpected unmounts: %#v", unmounted)
	}
	if _, ok := lm.refCounter[refspec.String()][dgst.String()]; ok {
		t.Errorf("layer entry not removed from refCounter")
	}
	if _, ok := lm.refCounter[refspec.String()]; ok {
		t.Errorf("ref entry not removed from refCounter")
	}
	if _, ok := lm.nydusMetaLayer.Load(snapshotID + ":" + dgst.String()); ok {
		t.Errorf("nydusMetaLayer entry not deleted")
	}
}
