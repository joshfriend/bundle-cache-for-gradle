set positional-arguments := true
set shell := ["bash", "-c"]

VERSION := `git describe --tags --always --dirty 2>/dev/null || echo "dev"`
RELEASE := "dist"
GOARCH := env("GOARCH", `go env GOARCH`)
GOOS := env("GOOS", `go env GOOS`)

_help:
    @just -l

# Run tests
test:
    go test ./...

# Lint code
lint:
    golangci-lint run

# Format code
fmt:
    git ls-files | grep '\.go$' | xargs gofmt -w
    go mod tidy

# Build for current platform
build GOOS=(GOOS) GOARCH=(GOARCH):
    #!/usr/bin/env bash
    set -euo pipefail
    mkdir -p {{ RELEASE }}
    echo "Building dist/gradle-cache-{{ GOOS }}-{{ GOARCH }}"
    CGO_ENABLED=0 GOOS={{ GOOS }} GOARCH={{ GOARCH }} \
        go build -trimpath -o {{ RELEASE }}/gradle-cache-{{ GOOS }}-{{ GOARCH }} \
        -ldflags "-s -w -X main.version={{ VERSION }}" \
        ./cmd/gradle-cache
    test "{{ GOOS }}-{{ GOARCH }}" = "$(go env GOOS)-$(go env GOARCH)" && \
        (cd {{ RELEASE }} && ln -sf gradle-cache-{{ GOOS }}-{{ GOARCH }} gradle-cache)
    echo "Done"

# Build all platforms
build-all:
    @mkdir -p {{ RELEASE }}
    just build darwin arm64
    just build darwin amd64
    just build linux arm64
    just build linux amd64

# Build macOS universal binary (arm64 + amd64 in one Mach-O fat binary).
# Requires darwin binaries to exist first (e.g. via build-all or build-fat).
# Uses tools/makefat so this works on any platform, including Linux CI.
build-universal:
    go run ./tools/makefat \
        {{ RELEASE }}/gradle-cache-darwin-universal \
        {{ RELEASE }}/gradle-cache-darwin-amd64 \
        {{ RELEASE }}/gradle-cache-darwin-arm64
    @echo "Built {{ RELEASE }}/gradle-cache-darwin-universal"

# Build a single fat binary that runs on Linux and macOS (amd64/arm64).
# The wrapper script (scripts/gradle-cache) detects OS/arch at runtime,
# finds the __ARCHIVE__ sentinel, pipes the remainder straight to tar, and
# execs the extracted binary from a version-tagged cache directory.
build-fat: build-all
    #!/usr/bin/env bash
    set -euo pipefail
    RELEASE="{{ RELEASE }}"
    VERSION="{{ VERSION }}"
    OUT="$RELEASE/gradle-cache"

    # Remove any existing symlink left by the per-platform build before writing
    rm -f "$OUT"

    # Write the wrapper and inject the build-time version
    sed "s/__VERSION__/$VERSION/g" scripts/gradle-cache > "$OUT"

    # Append all platform binaries as a single gzip-compressed tarball
    tar czf - \
        -C "$RELEASE" \
        gradle-cache-linux-amd64 \
        gradle-cache-linux-arm64 \
        gradle-cache-darwin-amd64 \
        gradle-cache-darwin-arm64 \
        >> "$OUT"

    chmod +x "$OUT"
    SIZE=$(du -sh "$OUT" | cut -f1)
    echo "Built fat binary: $OUT ($SIZE)"

# Clean build artifacts
clean:
    rm -rf {{ RELEASE }}
