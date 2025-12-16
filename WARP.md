# WARP.md

This file provides guidance to WARP (warp.dev) when working with code in this repository.

## Common commands

- Build (Go 1.25 pinned via `go.mod` and CI):
  - Default (linux/amd64): `make build`
  - Cross-compile examples: `GOOS=linux GOARCH=arm64 make build`
  - Output: `bin/nydus-store`
- Lint (golangci-lint uses .golangci.yml):
  - `make check`
  - If missing locally: `curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/HEAD/install.sh | sh -s -- -b "$(go env GOPATH)"/bin v2.5.0`
- Unit tests:
  - All: `go test ./...`
  - Package: `go test ./pkg/fs -v`
  - Single test: `go test ./pkg/manager -run '^TestName$' -v`
- Integration smoke (Linux with Podman and root privileges):
  - Install nydus (version used in CI):
    - `NYDUS_VERSION=v2.1.6`
    - `wget https://github.com/dragonflyoss/image-service/releases/download/$NYDUS_VERSION/nydus-static-$NYDUS_VERSION-linux-amd64.tgz`
    - `sudo tar xzvf nydus-static-$NYDUS_VERSION-linux-amd64.tgz --wildcards --strip-components=1 -C /usr/bin/ nydus-static/*`
  - Configure storage and nydusd:
    - `sudo mkdir -p /var/lib/nydus-store`
    - `sudo cp misc/nydus-config.json /etc/nydusd-config.json`
    - `sudo cp misc/storage.conf /etc/containers/storage.conf`
  - Run store and verify:
    - `nohup sudo bin/nydus-store --log-to-stdout --log-level info --config-path /etc/nydusd-config.json --root /var/lib/nydus-store &`
    - `sudo podman run -it ghcr.io/dragonflyoss/image-service/nginx:nydus-latest echo hello word`

## Running the plugin locally

- Requires Linux kernel with FUSE, Podman/CRI-O using containers/storage, and `nydusd`/`nydus-image` installed in PATH.
- Typical launch:
  - `sudo bin/nydus-store --log-to-stdout --log-level info --config-path /etc/nydusd-config.json --root /var/lib/nydus-store`
- Optional file mode overrides (octal without leading 0o):
  - `--fs-file-mode 0400 --fs-dir-mode 0500 --fs-link-mode 0400`
- Credentials resolution obeys (in order):
  - Docker config.json
  - Podman-compatible `auth.json`: `REGISTRY_AUTH_FILE`, `$XDG_RUNTIME_DIR/containers/auth.json`, `$HOME/.config/containers/auth.json`

## High-level architecture

- Entry point (`cmd/store/main.go`)
  - Uses flags from `containerd/nydus-snapshotter` to parse config (`--root`, `--config-path`, logging), sets up `slog`.
  - Builds a registry resolver with credentials from both Docker config and Podman `auth.json`.
  - Initializes the `LayerManager`, mounts a FUSE filesystem under `<root>/store`, and blocks until SIGINT. On exit, releases mounts.
- Resolver & Keychains (`pkg/services/...`)
  - `resolver.RegistryHostsFromConfig` constructs `docker.RegistryHost` with retryable HTTP client and request timeouts.
  - Credential sources:
    - Docker: `pkg/services/keychain/dockerconfig` (supports identity token, user/pass; Docker Hub host normalized).
    - Podman: `pkg/services/keychain/podmanauth` (searches `REGISTRY_AUTH_FILE`, XDG runtime, then `$HOME/.config/containers/auth.json`; supports identity token and base64 `auth`).
- Layer management (`pkg/manager`)
  - Verifies signatures (configurable public key, optional validation).
  - Spawns and tracks `nydusd` processes via snapshotter’s process manager; stores state in an embedded DB under `<root>`.
  - Resolves image manifests/configs, identifies Nydus meta layers, downloads bootstrap, mounts via Nydus FS, waits for readiness.
  - Exposes mounted content by bind-mounting Nydus mountpoints to `<root>/store/<snapshotID>/<digest>/diff` (read-only), reference-counted per layer.
  - Crash recovery: on startup, attempts to unmount any orphaned bind mounts found under `<root>/store/*/*/diff`.
  - OS-specific shims: Linux-specific mount helpers with `mount_shim_linux.go`, safe fallbacks in `mount_shim_other.go` for non-Linux builds.
- FUSE filesystem (`pkg/fs`)
  - go-fuse v2 based; presents a structured view with directories and symlinks: `pool`, `diff`, `blob`, `info`, and `use` markers.
  - Default permission modes are intentionally restrictive; can be overridden at mount time via `WithModes` (wired to CLI flags).
  - Detects `fusermount`/`fusermount3`; if absent, attempts direct mount. Waits for server mount completion before returning.
- Integration with containers/storage
  - `misc/storage.conf` declares `additionallayerstores = [ "/var/lib/nydus-store/store:ref" ]` to register the plugin’s store.
  - Podman/CRI-O can then lazy-mount Nydus layers referenced by images.

## Project rules for Warp agents

- Use Go 1.25 toolchain.
- Maintain compatibility with Podman `auth.json` discovery and precedence; do not regress Docker config support.
- Prefer FUSE3 (`fusermount3`) when available; fallback paths must remain functional.
- Keep FS access modes configurable via CLI flags and plumbed through `pkg/fs`.
- Preserve and improve crash/unmount recovery semantics in `LayerManager` (e.g., `RecoverOrphanMounts`, `ReleaseAll`).

## Notable files

- `Makefile` — build and lint targets (`build`, `check`).
- `.golangci.yml` — enabled linters/formatters.
- `misc/nydus-config.json`, `misc/storage.conf` — sample runtime configs.
- `cmd/store/main.go` — CLI entry.
- `pkg/manager/*` — layer lifecycle, mounting, recovery.
- `pkg/fs/*` — FUSE filesystem and wiring.
- `pkg/services/{resolver,keychain}/` — registry access and auth.
