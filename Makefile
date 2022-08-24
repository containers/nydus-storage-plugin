GOOS ?= linux
GOARCH ?= amd64

.PHONY: build
build:
	GOOS=${GOOS} GOARCH=${GOARCH} ${PROXY} go build -ldflags="-s -w -X 'main.Version=${VERSION}'" -v -o bin/nydus-store ./cmd/store

.PHONY: check
check:
	golangci-lint run
