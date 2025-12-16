package podmanauth

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/containerd/containerd/log"
	"github.com/containerd/containerd/reference"

	"github.com/containers/nydus-storage-plugin/pkg/services/resolver"
)

// NewPodmanAuthKeychain returns a resolver.Credential that sources credentials
// from Podman-compatible auth.json files.
// Precedence:
// 1) REGISTRY_AUTH_FILE (path to an auth.json)
// 2) $XDG_RUNTIME_DIR/containers/auth.json
// 3) $HOME/.config/containers/auth.json
func NewPodmanAuthKeychain(ctx context.Context) resolver.Credential {
	return func(host string, _ reference.Spec) (string, string, error) {
		path, err := findAuthFile()
		if err != nil || path == "" {
			return "", "", nil
		}
		creds, err := readAuthFile(path)
		if err != nil {
			log.G(ctx).WithError(err).Warnf("failed to read podman auth.json from %s", path)
			return "", "", nil
		}

		// Docker Hub special-case compatibility
		if host == "docker.io" || host == "registry-1.docker.io" {
			host = "https://index.docker.io/v1/"
		}

		if e, ok := creds.Auths[host]; ok {
			if e.IdentityToken != "" {
				return "", e.IdentityToken, nil
			}
			// Prefer explicit username/password if present
			if e.Username != "" || e.Password != "" {
				return e.Username, e.Password, nil
			}
			if e.Auth != "" {
				b, err := base64.StdEncoding.DecodeString(e.Auth)
				if err == nil {
					p := string(b)
					if idx := strings.IndexByte(p, ':'); idx >= 0 {
						return p[:idx], p[idx+1:], nil
					}
				}
			}
		}
		return "", "", nil
	}
}

// Minimal struct for containers-auth.json
type authFile struct {
	Auths map[string]authEntry `json:"auths"`
}

type authEntry struct {
	Auth          string `json:"auth"`
	Username      string `json:"username"`
	Password      string `json:"password"`
	IdentityToken string `json:"identitytoken"`
}

func findAuthFile() (string, error) {
	if p := os.Getenv("REGISTRY_AUTH_FILE"); p != "" {
		return p, nil
	}
	if xdg := os.Getenv("XDG_RUNTIME_DIR"); xdg != "" {
		p := filepath.Join(xdg, "containers", "auth.json")
		if _, err := os.Stat(p); err == nil {
			return p, nil
		}
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	p := filepath.Join(home, ".config", "containers", "auth.json")
	if _, err := os.Stat(p); err == nil {
		return p, nil
	}
	return "", nil
}

func readAuthFile(path string) (authFile, error) {
	f, err := os.Open(path)
	if err != nil {
		return authFile{}, err
	}
	defer f.Close()
	var af authFile
	if err := json.NewDecoder(f).Decode(&af); err != nil {
		return authFile{}, err
	}
	if af.Auths == nil {
		return authFile{}, errors.New("no auths")
	}
	return af, nil
}
