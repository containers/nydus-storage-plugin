package main

import (
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"syscall"

	"github.com/containerd/nydus-snapshotter/cmd/containerd-nydus-grpc/pkg/command"
	"github.com/containerd/nydus-snapshotter/config"
	"github.com/containerd/nydus-snapshotter/pkg/errdefs"
	"github.com/pkg/errors"
	"github.com/urfave/cli/v2"

	"github.com/containers/nydus-storage-plugin/pkg/fs"
	"github.com/containers/nydus-storage-plugin/pkg/manager"
	"github.com/containers/nydus-storage-plugin/pkg/services/keychain/dockerconfig"
	podmanauth "github.com/containers/nydus-storage-plugin/pkg/services/keychain/podmanauth"
	"github.com/containers/nydus-storage-plugin/pkg/services/resolver"
)

func waitForSIGINT() {
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt)
	<-c
}

func parseOctalMode(s string) (uint32, error) {
	var v uint32
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c < '0' || c > '7' {
			return 0, fmt.Errorf("invalid octal: %s", s)
		}
		v = (v << 3) | uint32(c-'0')
	}
	return v, nil
}

func setupSlog(level string, toStdout bool, logDir string) error {
	var lvl slog.Level
	switch level {
	case "debug":
		lvl = slog.LevelDebug
	case "warn", "warning":
		lvl = slog.LevelWarn
	case "error":
		lvl = slog.LevelError
	default:
		lvl = slog.LevelInfo
	}
	var w *os.File
	if toStdout || logDir == "" {
		w = os.Stdout
	} else {
		path := filepath.Join(logDir, "nydus-store.log")
		f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
		if err != nil {
			return err
		}
		w = f
	}
	h := slog.NewTextHandler(w, &slog.HandlerOptions{Level: lvl})
	slog.SetDefault(slog.New(h))
	return nil
}

func main() {
	flags := command.NewFlags()
	app := &cli.App{
		Name:    "crio nydus store",
		Usage:   "crio nydus store plugin",
		Version: "0.0.0",
		Flags: append(flags.F,
			&cli.StringFlag{Name: "fs-file-mode", Usage: "octal file mode for files (e.g. 0400)"},
			&cli.StringFlag{Name: "fs-dir-mode", Usage: "octal dir mode (e.g. 0500)"},
			&cli.StringFlag{Name: "fs-link-mode", Usage: "octal symlink mode (e.g. 0400)"},
			&cli.BoolFlag{Name: "fs-allow-other", Value: true, Usage: "enable allow_other on FUSE mount"},
			&cli.BoolFlag{Name: "fs-direct-mount", Value: false, Usage: "force direct mount (bypass fusermount)"},
			&cli.BoolFlag{Name: "fs-mount-suid", Value: false, Usage: "add suid to fusermount mount options"},
		),
Action: func(c *cli.Context) error {
			if err := setupSlog(flags.Args.LogLevel, flags.Args.LogToStdout, flags.Args.LogDir); err != nil {
				return errors.Wrap(err, "failed to prepare logger")
			}

			// Fail fast on unsupported platforms to avoid false-positive "mounted" states.
			if runtime.GOOS != "linux" {
				slog.ErrorContext(c.Context, "nydus-store requires Linux (FUSE) to mount; current OS unsupported", "GOOS", runtime.GOOS)
				return errors.New("platform not supported: requires linux with FUSE")
			}

			var cfg config.Config
			if err := command.Validate(flags.Args, &cfg); err != nil {
				return errors.Wrap(err, "invalid argument")
			}

			mountPoint := fmt.Sprintf("%s/store", flags.Args.RootDir)
			if err := os.MkdirAll(mountPoint, 0755); err != nil {
				return errors.Wrapf(err, "create root directory %s", mountPoint)
			}

			// replace it with nydus-snapshotter resolver.
			hosts := resolver.RegistryHostsFromConfig(
				[]resolver.Credential{
					dockerconfig.NewDockerconfigKeychain(c.Context),
					// Podman-compatible auth.json
					podmanauth.NewPodmanAuthKeychain(c.Context),
				}...,
			)
			layManager, err := manager.NewLayerManager(c.Context, flags.Args.RootDir, hosts, &cfg)
			if err != nil {
				panic(err)
			}

			// Parse optional FS modes
			fileMode := fs.DefaultFileMode()
			dirMode := fs.DefaultDirMode()
			linkMode := fs.DefaultLinkMode()
			if v := c.String("fs-file-mode"); v != "" {
				if m, err := parseOctalMode(v); err == nil {
					fileMode = m
				}
			}
			if v := c.String("fs-dir-mode"); v != "" {
				if m, err := parseOctalMode(v); err == nil {
					dirMode = m
				}
			}
			if v := c.String("fs-link-mode"); v != "" {
				if m, err := parseOctalMode(v); err == nil {
					linkMode = m
				}
			}

			// Recover orphan bind mounts from previous crashes
			_ = layManager.RecoverOrphanMounts(c.Context)

			slog.InfoContext(c.Context, "Starting FUSE mount",
				"mountPoint", mountPoint,
				"rootDir", flags.Args.RootDir,
				"fileMode", fmt.Sprintf("0%o", fileMode),
				"dirMode", fmt.Sprintf("0%o", dirMode),
				"linkMode", fmt.Sprintf("0%o", linkMode),
				"allowOther", c.Bool("fs-allow-other"))

			if err := fs.Mount(
				c.Context,
				mountPoint,
				flags.Args.RootDir,
				true,
				layManager,
				fs.WithModes(fileMode, dirMode, linkMode),
				fs.WithAllowOther(c.Bool("fs-allow-other")),
				fs.WithDirectMount(c.Bool("fs-direct-mount")),
				fs.WithMountSuid(c.Bool("fs-mount-suid")),
			); err != nil {
				slog.ErrorContext(c.Context, "failed to mount fs", "mountPoint", mountPoint, "err", err)
				return err
			}
			defer func() {
				// Best-effort: release bind mounts first
				layManager.ReleaseAll(c.Context)
				// Try a normal unmount of the FUSE mountpoint
				if err := syscall.Unmount(mountPoint, 0); err != nil {
					slog.WarnContext(c.Context, "unmount busy; retry lazy unmount", "err", err)
					if derr := lazyUnmount(mountPoint); derr != nil {
						slog.ErrorContext(c.Context, "lazy unmount failed", "err", derr)
					}
				}
				slog.InfoContext(c.Context, "Exiting")
			}()
			waitForSIGINT()
			return nil
		},
	}
	if err := app.Run(os.Args); err != nil {
		if errdefs.IsConnectionClosed(err) {
			slog.Info("snapshotter exited")
			return
		}
		slog.Error("failed to start crio nydus store", "err", err)
	}
}
