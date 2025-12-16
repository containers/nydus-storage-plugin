package dockerconfig

import (
	"context"
	"log/slog"

	"github.com/containerd/containerd/reference"
	"github.com/docker/cli/cli/config"

	"github.com/containers/nydus-storage-plugin/pkg/services/resolver"
)

// Ported from stargz-snapshotter, copyright The stargz-snapshotter Authors.
// https://github.com/containerd/stargz-snapshotter/blob/923399007a8cde1ec871072ba6678b428b40b852/service/keychain/dockerconfig/dockerconfig.go
func NewDockerconfigKeychain(ctx context.Context) resolver.Credential {
	return func(host string, _ reference.Spec) (string, string, error) {
		cf, err := config.Load("")
		if err != nil {
			slog.WarnContext(ctx, "failed to load docker config file", "err", err)
			return "", "", nil
		}

		if host == "docker.io" || host == "registry-1.docker.io" {
			// Creds of docker.io is stored keyed by "https://index.docker.io/v1/".
			host = "https://index.docker.io/v1/"
		}
		ac, err := cf.GetAuthConfig(host)
		if err != nil {
			return "", "", err
		}
		if ac.IdentityToken != "" {
			return "", ac.IdentityToken, nil
		}
		return ac.Username, ac.Password, nil
	}
}
