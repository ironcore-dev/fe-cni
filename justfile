BUILD_DIR := "build"
BIN_NAME := "fecni"
CMD_DIR := "./cmd/fecni"
INIT_BIN_NAME := "fecni-init"
INIT_CMD_DIR := "./cmd/fecni-init"

# feCNI is a Linux CNI plugin — always target Linux.
GOOS := "linux"
GOARCH := env("GOARCH", "amd64")

VERSION := `cat VERSION 2>/dev/null || echo "dev"`
BUILD_TIME := `date -u +"%Y-%m-%dT%H:%M:%SZ"`
GIT_COMMIT := `git rev-parse --short HEAD 2>/dev/null || echo "unknown"`

[default]
list:
    just --list

# Build the feCNI binaries
build:
    CGO_ENABLED=0 GOOS={{GOOS}} GOARCH={{GOARCH}} go build \
        -ldflags "-s -w -X main.Version={{VERSION}} -X main.BuildTime={{BUILD_TIME}} -X main.GitCommit={{GIT_COMMIT}}" \
        -o {{ BUILD_DIR }}/{{ BIN_NAME }} {{ CMD_DIR }}
    CGO_ENABLED=0 GOOS={{GOOS}} GOARCH={{GOARCH}} go build \
        -ldflags "-s -w -X main.Version={{VERSION}} -X main.BuildTime={{BUILD_TIME}} -X main.GitCommit={{GIT_COMMIT}}" \
        -o {{ BUILD_DIR }}/{{ INIT_BIN_NAME }} {{ INIT_CMD_DIR }}

# Run tests
test:
    CGO_ENABLED=0 GOOS={{GOOS}} GOARCH={{GOARCH}} go test ./...

# Run code linters
lint:
    CGO_ENABLED=0 GOOS={{GOOS}} GOARCH={{GOARCH}} golangci-lint run

# Format Go code
fmt:
    CGO_ENABLED=0 GOOS={{GOOS}} GOARCH={{GOARCH}} golangci-lint fmt

# Clean build artifacts
clean:
    rm -rf {{ BUILD_DIR }}

# Build container image
image:
    docker buildx build --platform {{GOOS}}/{{GOARCH}} -t fecni:{{VERSION}} --load .
    docker save fecni:{{ VERSION }} -o {{ BUILD_DIR }}/fecni_{{GOOS}}_{{GOARCH}}_{{ VERSION }}.tar
