#!/bin/bash
# Release script for Muti Metroo
# Bumps version, tags, cross-compiles, creates GitHub release with assets
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
cd "$SCRIPT_DIR"

# --- Configuration ---
GITHUB_URL="https://github.com"
REPO="Muti-Metroo/Muti-Metroo"
VERSION_FILE="VERSION"
BINARY="muti-metroo"
TOKEN="${GITHUB_TOKEN:?GITHUB_TOKEN env var is required}"

# --- Parse arguments ---
BUMP="${1:-patch}"  # patch (default), minor, or major
case "$BUMP" in
    patch|minor|major) ;;
    *) echo "Usage: $0 [patch|minor|major]"; exit 1 ;;
esac

# --- Read and bump version ---
CURRENT_VERSION=$(cat "$VERSION_FILE" | tr -d '[:space:]')
IFS='.' read -r MAJOR MINOR PATCH <<< "$CURRENT_VERSION"

case "$BUMP" in
    major) MAJOR=$((MAJOR + 1)); MINOR=0; PATCH=0 ;;
    minor) MINOR=$((MINOR + 1)); PATCH=0 ;;
    patch) PATCH=$((PATCH + 1)) ;;
esac

NEW_VERSION="${MAJOR}.${MINOR}.${PATCH}"
TAG="v${NEW_VERSION}"

echo "=== Muti Metroo Release ==="
echo "Current version: $CURRENT_VERSION"
echo "Bump type:       $BUMP"
echo "New version:     $NEW_VERSION"
echo "Tag:             $TAG"
echo ""

# --- Pre-flight checks ---
echo "=== Pre-flight checks ==="

# Required tools
for cmd in go jq curl; do
    if ! command -v "$cmd" &>/dev/null; then
        echo "Error: $cmd is required but not found"
        exit 1
    fi
done
echo "  Required tools: OK"

# Uncommitted changes (excluding VERSION)
if ! git diff --quiet --exit-code -- ':!VERSION' || ! git diff --cached --quiet --exit-code -- ':!VERSION'; then
    echo "Error: You have uncommitted changes. Commit or stash them first."
    git status --short
    exit 1
fi
echo "  Working tree: clean"

# Tag existence
if git tag -l "$TAG" | grep -q .; then
    echo "Error: Tag $TAG already exists"
    exit 1
fi
echo "  Tag $TAG: available"

# Optional tools
HAS_UPX=false
UPX_VIA_DOCKER=false
if command -v upx &>/dev/null; then
    HAS_UPX=true
    echo "  UPX: found (native)"
elif command -v docker &>/dev/null; then
    HAS_UPX=true
    UPX_VIA_DOCKER=true
    echo "  UPX: found (via Docker)"
else
    echo "  UPX: not found (binaries will not be compressed)"
fi

HAS_MINGW_AMD64=false
MINGW_AMD64_VIA_DOCKER=false
if command -v x86_64-w64-mingw32-gcc &>/dev/null; then
    HAS_MINGW_AMD64=true
    echo "  mingw-w64 (amd64): found (native)"
elif command -v docker &>/dev/null; then
    HAS_MINGW_AMD64=true
    MINGW_AMD64_VIA_DOCKER=true
    echo "  mingw-w64 (amd64): found (via Docker)"
else
    echo "  mingw-w64 (amd64): not found (amd64 DLL build will be skipped)"
fi

# Windows ARM64 needs llvm-mingw (gcc-mingw-w64 has no aarch64 variant).
# Detect either the clang or gcc wrapper from an llvm-mingw install.
HAS_MINGW_ARM64=false
MINGW_ARM64_VIA_DOCKER=false
NATIVE_ARM64_CC=""
if command -v aarch64-w64-mingw32-clang &>/dev/null; then
    HAS_MINGW_ARM64=true
    NATIVE_ARM64_CC="aarch64-w64-mingw32-clang"
    echo "  llvm-mingw (arm64): found (native)"
elif command -v aarch64-w64-mingw32-gcc &>/dev/null; then
    HAS_MINGW_ARM64=true
    NATIVE_ARM64_CC="aarch64-w64-mingw32-gcc"
    echo "  llvm-mingw (arm64): found (native)"
elif command -v docker &>/dev/null; then
    HAS_MINGW_ARM64=true
    MINGW_ARM64_VIA_DOCKER=true
    echo "  llvm-mingw (arm64): found (via Docker)"
else
    echo "  llvm-mingw (arm64): not found (arm64 DLL build will be skipped)"
fi

HAS_PDF_DEPS=false
if command -v pandoc &>/dev/null && command -v node &>/dev/null && (command -v xelatex &>/dev/null || command -v tectonic &>/dev/null); then
    HAS_PDF_DEPS=true
    echo "  PDF deps: found"
else
    echo "  PDF deps: missing (user manual build will be skipped)"
fi

# SHA256 command (macOS vs Linux)
if command -v sha256sum &>/dev/null; then
    SHA256CMD="sha256sum"
else
    SHA256CMD="shasum -a 256"
fi

echo ""

# --- Cross-compile all targets ---
echo "=== Building CLI binaries ==="
LDFLAGS="-s -w -buildid= -X main.Version=${NEW_VERSION}"

# Format: os:arch:ext:upx
TARGETS=(
    "darwin:arm64::false"
    "darwin:amd64::false"
    "linux:amd64::true"
    "linux:arm64::true"
    "windows:amd64:.exe:false"
    "windows:arm64:.exe:false"
)

rm -rf dist/
mkdir -p dist/

for target in "${TARGETS[@]}"; do
    IFS=':' read -r os arch ext do_upx <<< "$target"
    output="dist/${BINARY}-${os}-${arch}${ext}"
    echo -n "  Building ${os}/${arch}... "
    CGO_ENABLED=0 GOOS="$os" GOARCH="$arch" go build -trimpath -buildvcs=false \
        -ldflags="$LDFLAGS" -o "$output" ./cmd/muti-metroo
    echo "OK ($(du -h "$output" | cut -f1 | xargs))"
done

echo ""
echo "Built $(ls dist/ | wc -l | tr -d ' ') binaries"

# --- Verify no developer-environment leaks ---
# Greps the binary for absolute developer paths. The grep -o + grep -v pair
# extracts just the matching path (not the surrounding string-table line) and
# discards the documented placeholder /home/user/uploads that appears in the
# allowed_paths help text. A real leak (e.g. /home/andris/, /Users/andris/...)
# will pass through and abort the release.
echo ""
echo "=== Verifying no developer-environment leaks ==="
LEAK_PATTERNS='/Users/[A-Za-z][A-Za-z0-9_-]+/[A-Za-z]|/home/[a-z][a-z0-9_-]+/[A-Z]|/private/var/folders/[a-z0-9_]+/|GOROOT=[^ ]+'
LEAK_FOUND=0
for f in dist/muti-metroo-*; do
    [ -f "$f" ] || continue
    # `|| true`: with `set -euo pipefail`, an empty grep pipeline returns 1
    # and silently aborts the release on the happy path. We want LEAKS="" in
    # that case so the per-file `if [ -n ... ]` below decides what to do.
    LEAKS=$(strings "$f" | grep -oE "$LEAK_PATTERNS" | grep -vE '^/home/user/' | sort -u || true)
    if [ -n "$LEAKS" ]; then
        echo "  LEAK in $f:"
        echo "$LEAKS" | head -5 | sed 's/^/    /'
        LEAK_FOUND=1
    fi
done
if [ $LEAK_FOUND -eq 1 ]; then
    echo "Error: developer paths leaked into release binaries -- aborting"
    exit 1
fi
echo "  All binaries clean"

# --- UPX compression ---
echo ""
if $HAS_UPX; then
    echo "=== Compressing binaries with UPX ==="

    # Collect files to compress
    UPX_FILES=()
    for target in "${TARGETS[@]}"; do
        IFS=':' read -r os arch ext do_upx <<< "$target"
        if [ "$do_upx" = "true" ]; then
            UPX_FILES+=("${BINARY}-${os}-${arch}${ext}")
        fi
    done

    if $UPX_VIA_DOCKER; then
        # Compress all eligible binaries in a single Docker run
        echo "  Using Docker (ubuntu:latest + upx)..."
        DOCKER_CMD="apt-get update -qq && apt-get install -y -qq upx-ucl > /dev/null 2>&1"
        for f in "${UPX_FILES[@]}"; do
            DOCKER_CMD+=" && echo -n '  Compressing ${f}... ' && upx --best --lzma /dist/${f} > /dev/null 2>&1 && echo 'OK' || echo 'FAILED'"
        done
        docker run --rm -v "$(pwd)/dist:/dist" ubuntu:latest bash -c "$DOCKER_CMD"
    else
        for f in "${UPX_FILES[@]}"; do
            echo -n "  Compressing ${f}... "
            if upx --best --lzma "dist/${f}" > /dev/null 2>&1; then
                echo "OK ($(du -h "dist/${f}" | cut -f1 | xargs))"
            else
                echo "FAILED (keeping uncompressed)"
            fi
        done
    fi

    # Print compressed sizes
    echo "  Compressed sizes:"
    for f in "${UPX_FILES[@]}"; do
        echo "    ${f}: $(du -h "dist/${f}" | cut -f1 | xargs)"
    done
else
    echo "=== Skipping UPX compression (upx not found, no Docker) ==="
fi

# --- Build DLLs ---
# Pinned llvm-mingw image tag used as the Docker fallback for the ARM64 DLL.
# llvm-mingw provides aarch64-w64-mingw32-clang, which gcc-mingw-w64 does not.
LLVM_MINGW_IMAGE="mstorsjo/llvm-mingw:latest"

build_amd64_dll() {
    echo "=== Building Windows DLL (amd64) ==="
    local out="dist/${BINARY}-windows-amd64.dll"
    echo -n "  Cross-compiling DLL... "
    set +e
    if $MINGW_AMD64_VIA_DOCKER; then
        local go_version
        go_version=$(grep '^go ' go.mod | awk '{print $2}')
        docker run --rm \
            -v "$(pwd):/src" -w /src \
            -e CGO_ENABLED=1 \
            -e CC=x86_64-w64-mingw32-gcc \
            -e GOOS=windows \
            -e GOARCH=amd64 \
            golang:${go_version}-bookworm bash -c \
            "apt-get update -qq && apt-get install -y -qq gcc-mingw-w64-x86-64 > /dev/null 2>&1 && \
             go build -buildmode=c-shared -trimpath -buildvcs=false \
             -ldflags='$LDFLAGS' \
             -o $out ./cmd/muti-dll" 2>/dev/null
    else
        CGO_ENABLED=1 CC=x86_64-w64-mingw32-gcc GOOS=windows GOARCH=amd64 \
            go build -buildmode=c-shared -trimpath -buildvcs=false \
            -ldflags="$LDFLAGS" \
            -o "$out" ./cmd/muti-dll 2>/dev/null
    fi
    local rc=$?
    set -e
    if [ $rc -eq 0 ]; then
        rm -f "dist/${BINARY}-windows-amd64.h"
        echo "OK ($(du -h "$out" | cut -f1 | xargs))"
        compress_dll "$out"
        return 0
    fi
    echo "FAILED (continuing without amd64 DLL)"
    return 1
}

build_arm64_dll() {
    echo "=== Building Windows DLL (arm64) ==="
    local out="dist/${BINARY}-windows-arm64.dll"
    echo -n "  Cross-compiling DLL... "
    set +e
    if $MINGW_ARM64_VIA_DOCKER; then
        local go_version
        go_version=$(grep '^go ' go.mod | awk '{print $2}')
        # mstorsjo/llvm-mingw ships the aarch64-w64-mingw32 toolchain in PATH
        # at /opt/llvm-mingw/bin; we layer the official Go tarball on top so
        # the Go version matches go.mod exactly.
        docker run --rm \
            -v "$(pwd):/src" -w /src \
            -e CGO_ENABLED=1 \
            -e CC=aarch64-w64-mingw32-clang \
            -e GOOS=windows \
            -e GOARCH=arm64 \
            "$LLVM_MINGW_IMAGE" bash -c \
            "apt-get update -qq && apt-get install -y -qq curl ca-certificates > /dev/null 2>&1 && \
             curl -sL https://go.dev/dl/go${go_version}.linux-amd64.tar.gz | tar -xz -C /usr/local && \
             export PATH=/usr/local/go/bin:\$PATH && \
             go build -buildmode=c-shared -trimpath -buildvcs=false \
             -ldflags='$LDFLAGS' \
             -o $out ./cmd/muti-dll" 2>/dev/null
    else
        CGO_ENABLED=1 CC="$NATIVE_ARM64_CC" GOOS=windows GOARCH=arm64 \
            go build -buildmode=c-shared -trimpath -buildvcs=false \
            -ldflags="$LDFLAGS" \
            -o "$out" ./cmd/muti-dll 2>/dev/null
    fi
    local rc=$?
    set -e
    if [ $rc -eq 0 ]; then
        rm -f "dist/${BINARY}-windows-arm64.h"
        echo "OK ($(du -h "$out" | cut -f1 | xargs))"
        compress_dll "$out"
        return 0
    fi
    echo "FAILED (continuing without arm64 DLL)"
    return 1
}

compress_dll() {
    local out="$1"
    if ! $HAS_UPX; then
        return 0
    fi
    echo -n "  Compressing DLL with UPX... "
    local name
    name=$(basename "$out")
    if $UPX_VIA_DOCKER; then
        docker run --rm -v "$(pwd)/dist:/dist" ubuntu:latest bash -c \
            "apt-get update -qq && apt-get install -y -qq upx-ucl > /dev/null 2>&1 && upx --best --lzma /dist/${name} > /dev/null 2>&1" && \
            echo "OK ($(du -h "$out" | cut -f1 | xargs))" || echo "FAILED (keeping uncompressed)"
    elif upx --best --lzma "$out" > /dev/null 2>&1; then
        echo "OK ($(du -h "$out" | cut -f1 | xargs))"
    else
        echo "FAILED (keeping uncompressed)"
    fi
}

echo ""
if $HAS_MINGW_AMD64; then
    build_amd64_dll
else
    echo "=== Skipping amd64 DLL build (mingw-w64 not found, no Docker) ==="
fi

echo ""
if $HAS_MINGW_ARM64; then
    build_arm64_dll || true
else
    echo "=== Skipping arm64 DLL build (llvm-mingw not found, no Docker) ==="
fi

# --- Build user manual PDF ---
echo ""
if $HAS_PDF_DEPS; then
    echo "=== Building user manual PDF ==="

    # Resolve sibling project versions for download URLs
    MUTIAUK_VER=$(cat "$SCRIPT_DIR/../Mutiauk/VERSION" 2>/dev/null | tr -d '[:space:]' || echo "unknown")
    METROO_MANAGER_VER=$(cat "$SCRIPT_DIR/../Muti-Metroo-Manager/VERSION" 2>/dev/null | tr -d '[:space:]' || echo "unknown")

    # Patch download URL placeholders in user manual source
    echo "  Patching download URLs (Muti Metroo v${NEW_VERSION}, Entry v${MUTIAUK_VER}, Web v${METROO_MANAGER_VER})..."
    MANUAL_FILES=(user-manual/src/*.md)
    for f in "${MANUAL_FILES[@]}"; do
        if [ -f "$f" ]; then
            sed -i.bak \
                -e "s|__MUTI_METROO_RELEASE__|${GITHUB_URL}/${REPO}/releases/download/v${NEW_VERSION}|g" \
                -e "s|__MUTIAUK_RELEASE__|${GITHUB_URL}/Muti-Metroo/Mutiauk/releases/download/v${MUTIAUK_VER}|g" \
                -e "s|__METROO_MANAGER_RELEASE__|${GITHUB_URL}/Muti-Metroo/Muti-Metroo-Manager/releases/download/v${METROO_MANAGER_VER}|g" \
                "$f"
        fi
    done

    set +e
    echo -n "  Installing npm dependencies... "
    (cd user-manual && npm install --silent 2>/dev/null)
    NPM_RC=$?
    set -e
    if [ $NPM_RC -eq 0 ]; then
        echo "OK"
        echo "  Building PDF (this may take a moment)..."
        set +e
        (cd user-manual && make pdf VERSION="$NEW_VERSION" 2>&1 | sed 's/^/    /')
        PDF_RC=$?
        set -e
        if [ $PDF_RC -eq 0 ] && [ -f "user-manual/build/muti-metroo-user-manual.pdf" ]; then
            cp user-manual/build/muti-metroo-user-manual.pdf dist/
            echo "  PDF: OK ($(du -h dist/muti-metroo-user-manual.pdf | cut -f1 | xargs))"
        else
            echo "  PDF: FAILED (continuing without PDF)"
        fi
    else
        echo "FAILED (continuing without PDF)"
    fi

    # Restore original files
    for f in "${MANUAL_FILES[@]}"; do
        if [ -f "$f.bak" ]; then
            mv "$f.bak" "$f"
        fi
    done
else
    echo "=== Skipping user manual PDF (dependencies missing) ==="
fi

# --- Build operator manual PDF ---
echo ""
if $HAS_PDF_DEPS; then
    echo "=== Building operator manual PDF ==="

    # Patch download URL placeholders in operator manual source
    # (sibling project versions already loaded above for the user manual block)
    echo "  Patching download URLs (Muti Metroo v${NEW_VERSION}, Entry v${MUTIAUK_VER}, Web v${METROO_MANAGER_VER})..."
    OP_MANUAL_FILES=(operator-manual/src/*.md)
    for f in "${OP_MANUAL_FILES[@]}"; do
        if [ -f "$f" ]; then
            sed -i.bak \
                -e "s|__MUTI_METROO_RELEASE__|${GITHUB_URL}/${REPO}/releases/download/v${NEW_VERSION}|g" \
                -e "s|__MUTIAUK_RELEASE__|${GITHUB_URL}/Muti-Metroo/Mutiauk/releases/download/v${MUTIAUK_VER}|g" \
                -e "s|__METROO_MANAGER_RELEASE__|${GITHUB_URL}/Muti-Metroo/Muti-Metroo-Manager/releases/download/v${METROO_MANAGER_VER}|g" \
                "$f"
        fi
    done

    set +e
    echo -n "  Installing npm dependencies... "
    (cd operator-manual && npm install --silent 2>/dev/null)
    NPM_RC=$?
    set -e
    if [ $NPM_RC -eq 0 ]; then
        echo "OK"
        echo "  Building PDF (this may take a moment)..."
        set +e
        (cd operator-manual && make pdf VERSION="$NEW_VERSION" 2>&1 | sed 's/^/    /')
        PDF_RC=$?
        set -e
        if [ $PDF_RC -eq 0 ] && [ -f "operator-manual/build/muti-metroo-operator-manual.pdf" ]; then
            cp operator-manual/build/muti-metroo-operator-manual.pdf dist/
            echo "  PDF: OK ($(du -h dist/muti-metroo-operator-manual.pdf | cut -f1 | xargs))"
        else
            echo "  PDF: FAILED (continuing without PDF)"
        fi
    else
        echo "FAILED (continuing without PDF)"
    fi

    # Restore original files
    for f in "${OP_MANUAL_FILES[@]}"; do
        if [ -f "$f.bak" ]; then
            mv "$f.bak" "$f"
        fi
    done
else
    echo "=== Skipping operator manual PDF (dependencies missing) ==="
fi

# --- Build operator slideshow PDF ---
echo ""
if command -v node &>/dev/null && command -v npm &>/dev/null; then
    echo "=== Building operator slideshow PDF ==="

    set +e
    echo -n "  Installing npm dependencies... "
    (cd operator-slides && npm install --silent 2>/dev/null)
    NPM_RC=$?
    set -e
    if [ $NPM_RC -eq 0 ]; then
        echo "OK"
        echo "  Building slideshow PDF..."
        set +e
        (cd operator-slides && make pdf 2>&1 | sed 's/^/    /')
        SLIDES_RC=$?
        set -e
        if [ $SLIDES_RC -eq 0 ] && [ -f "operator-slides/build/muti-metroo-operator-slides.pdf" ]; then
            cp operator-slides/build/muti-metroo-operator-slides.pdf dist/
            echo "  PDF: OK ($(du -h dist/muti-metroo-operator-slides.pdf | cut -f1 | xargs))"
        else
            echo "  PDF: FAILED (continuing without slideshow)"
        fi
    else
        echo "FAILED (continuing without slideshow)"
    fi
else
    echo "=== Skipping operator slideshow PDF (Node.js / npm not available) ==="
fi

# --- Generate checksums ---
echo ""
echo "=== Generating checksums ==="
(cd dist && $SHA256CMD -- * > checksums.txt)
echo "  Generated checksums for $(wc -l < dist/checksums.txt | tr -d ' ') files"
cat dist/checksums.txt | sed 's/^/  /'

# --- Update VERSION file, commit, tag, push ---
echo ""
echo "=== Tagging and pushing ==="
echo "$NEW_VERSION" > "$VERSION_FILE"
git add "$VERSION_FILE"
git commit -m "Release $TAG"
echo "  Committed version bump to $NEW_VERSION"

git tag -a "$TAG" -m "Release $TAG"
echo "  Created tag $TAG"

git push origin master
git push origin "$TAG"
echo "  Pushed commit and tag to origin"

# --- Create GitHub release ---
echo ""
echo "=== Creating GitHub release ==="

RELEASE_BODY="## Muti Metroo $TAG

### CLI Binaries
- \`muti-metroo-darwin-arm64\` -- macOS ARM64 (Apple Silicon)
- \`muti-metroo-darwin-amd64\` -- macOS x86_64 (Intel)
- \`muti-metroo-linux-amd64\` -- Linux x86_64
- \`muti-metroo-linux-arm64\` -- Linux ARM64
- \`muti-metroo-windows-amd64.exe\` -- Windows x86_64
- \`muti-metroo-windows-arm64.exe\` -- Windows ARM64"

if [ -f "dist/${BINARY}-windows-amd64.dll" ] || [ -f "dist/${BINARY}-windows-arm64.dll" ]; then
    RELEASE_BODY+="

### Windows DLL"
    if [ -f "dist/${BINARY}-windows-amd64.dll" ]; then
        RELEASE_BODY+="
- \`muti-metroo-windows-amd64.dll\` -- Windows DLL (x86_64) for rundll32.exe integration"
    fi
    if [ -f "dist/${BINARY}-windows-arm64.dll" ]; then
        RELEASE_BODY+="
- \`muti-metroo-windows-arm64.dll\` -- Windows DLL (ARM64) for rundll32.exe integration"
    fi
fi

if [ -f "dist/muti-metroo-user-manual.pdf" ] || [ -f "dist/muti-metroo-operator-manual.pdf" ] || [ -f "dist/muti-metroo-operator-slides.pdf" ]; then
    RELEASE_BODY+="

### Documentation"
    if [ -f "dist/muti-metroo-operator-slides.pdf" ]; then
        RELEASE_BODY+="
- \`muti-metroo-operator-slides.pdf\` -- Operator slides (5-minute start for new operators)"
    fi
    if [ -f "dist/muti-metroo-operator-manual.pdf" ]; then
        RELEASE_BODY+="
- \`muti-metroo-operator-manual.pdf\` -- Operator Manual (practical usage guide)"
    fi
    if [ -f "dist/muti-metroo-user-manual.pdf" ]; then
        RELEASE_BODY+="
- \`muti-metroo-user-manual.pdf\` -- User Manual (feature reference)"
    fi
fi

RELEASE_BODY+="

### Verification
- \`checksums.txt\` -- SHA256 checksums of all artifacts

### Quick Start
\`\`\`bash
chmod +x muti-metroo-linux-amd64
./muti-metroo-linux-amd64 --help
\`\`\`"

create_release() {
    curl -s -X POST \
        "https://api.github.com/repos/${REPO}/releases" \
        -H "Authorization: token ${TOKEN}" \
        -H "Content-Type: application/json" \
        -d "$(jq -n \
            --arg tag "$TAG" \
            --arg name "Muti Metroo $TAG" \
            --arg body "$RELEASE_BODY" \
            '{tag_name: $tag, name: $name, body: $body, draft: false, prerelease: false}'
        )"
}

# Retry release creation (GitHub may need a moment after first push)
RELEASE_ID=""
for attempt in 1 2 3; do
    RELEASE_RESPONSE=$(create_release)
    RELEASE_ID=$(echo "$RELEASE_RESPONSE" | jq -r '.id')
    if [ "$RELEASE_ID" != "null" ] && [ -n "$RELEASE_ID" ]; then
        break
    fi
    if [ "$attempt" -lt 3 ]; then
        echo "  Attempt $attempt failed, retrying in 3s..."
        sleep 3
    fi
done

if [ "$RELEASE_ID" = "null" ] || [ -z "$RELEASE_ID" ]; then
    echo "Error: Failed to create release after 3 attempts"
    echo "$RELEASE_RESPONSE" | jq .
    exit 1
fi
echo "  Created release ID: $RELEASE_ID"

# --- Upload assets ---
echo ""
echo "=== Uploading release assets ==="

upload_asset() {
    local file="$1"
    local name
    name=$(basename "$file")
    echo -n "  Uploading $name... "
    local response
    response=$(curl -s -X POST \
        "https://uploads.github.com/repos/${REPO}/releases/${RELEASE_ID}/assets?name=${name}" \
        -H "Authorization: token ${TOKEN}" \
        -H "Content-Type: application/octet-stream" \
        --data-binary "@${file}")
    local asset_id
    asset_id=$(echo "$response" | jq -r '.id')
    if [ "$asset_id" = "null" ] || [ -z "$asset_id" ]; then
        echo "FAILED"
        echo "$response" | jq .
        return 1
    fi
    local size
    size=$(echo "$response" | jq -r '.size')
    echo "OK ($(numfmt --to=iec "$size" 2>/dev/null || echo "${size} bytes"))"
}

for file in dist/*; do
    upload_asset "$file"
done

# --- Verify release ---
echo ""
echo "=== Verifying release ==="
VERIFY=$(curl -s "https://api.github.com/repos/${REPO}/releases/tags/${TAG}" \
    -H "Authorization: token ${TOKEN}")
ASSET_COUNT=$(echo "$VERIFY" | jq '.assets | length')
echo "  Release: $(echo "$VERIFY" | jq -r '.name')"
echo "  Tag:     $(echo "$VERIFY" | jq -r '.tag_name')"
echo "  Assets:  $ASSET_COUNT"
echo "$VERIFY" | jq -r '.assets[] | "  - \(.name) (\(.size) bytes)"'

echo ""
echo "=== Release $TAG complete ==="
echo "URL: ${GITHUB_URL}/${REPO}/releases/tag/${TAG}"

