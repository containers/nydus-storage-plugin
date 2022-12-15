# CRI-O/Podman Nydus Store Plugin

The project is an implementation of additional layer store plugin of CRI-O/Podman, it provides CRI-O/Podman with the ability to lazy mount [nydus](https://nydus.dev/) images.

⚠️ This project is still in progress, see more details in this [proposal](https://github.com/containers/podman/issues/15249).

[![asciicast](https://asciinema.org/a/EqYr5HCcP5LndnFbKnBucPeqd.svg)](https://asciinema.org/a/EqYr5HCcP5LndnFbKnBucPeqd)


## Quick Start

1. Build store plugin

```shell
$ git clone https://github.com/containers/nydus-storage-plugin.git
$ cd nydus-storage-plugin
$ make
```

2. Install nydusd

Download nydus binaries from [nydus release](https://github.com/dragonflyoss/image-service/releases/) page, and then install with the command below:

```shell
$ tar xzvf nydus-static-$version-linux-amd64.tgz
$ sudo mv nydus-static/nydusd /usr/bin/nydusd
$ sudo mv nydus-static/nydus-image /usr/bin/nydus-image
```

3. Configure podman

Replace `/etc/containers/storage.conf` with `misc/storage.conf`.

4. Run store plugin

Copy `misc/nydusd-config.json` to `/etc`.

```shell
$ sudo bin/nydus-store --log-to-stdout --log-level info \
  --config-path /etc/nydusd-config.json \
  --root /var/lib/nydus-store
```

5. Run container with nydus image

```shell
$ sudo podman run -it ghcr.io/dragonflyoss/image-service/nginx:nydus-latest echo hello word
```

For the list of pre-converted nydus images, see [nydus packages](https://github.com/orgs/dragonflyoss/packages?page=1&repo_name=image-service), for more details about how to build nydus image, please refer to [nydusify](https://github.com/dragonflyoss/image-service/blob/master/docs/nydusify.md) conversion tool and [acceld](https://github.com/goharbor/acceleration-service).
