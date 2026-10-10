#!/bin/bash
# Publish Docusaurus site to srv-04.emailengine.dev
# Builds locally and deploys static files via rsync
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
SERVER="srv-04.emailengine.dev"
REMOTE_DIR="/var/www/muti-metroo"
STAGING_DIR="/tmp/muti-metroo-docs-deploy"

GITHUB_URL="https://github.com"

# --- Resolve latest versions from VERSION files ---
echo "=== Resolving latest versions ==="

read_version() {
    local file="$1"
    if [ -f "$file" ]; then
        cat "$file" | tr -d '[:space:]'
    else
        echo "unknown"
    fi
}

MUTI_METROO_VERSION=$(read_version "$SCRIPT_DIR/VERSION")
MUTIAUK_VERSION=$(read_version "$SCRIPT_DIR/../Mutiauk/VERSION")
METROO_MANAGER_VERSION=$(read_version "$SCRIPT_DIR/../Muti-Metroo-Manager/VERSION")

echo "  Muti Metroo:       $MUTI_METROO_VERSION"
echo "  Mutiauk: $MUTIAUK_VERSION"
echo "  Muti Metroo Manager:   $METROO_MANAGER_VERSION"

MUTI_METROO_DOWNLOAD="${GITHUB_URL}/postalsys/Muti-Metroo/releases/download/v${MUTI_METROO_VERSION}"
MUTIAUK_DOWNLOAD="${GITHUB_URL}/postalsys/Mutiauk/releases/download/v${MUTIAUK_VERSION}"
METROO_MANAGER_DOWNLOAD="${GITHUB_URL}/postalsys/Muti-Metroo-Manager/releases/download/v${METROO_MANAGER_VERSION}"

# --- Build Docusaurus site ---
echo ""
echo "=== Building Docusaurus site ==="
cd "$SCRIPT_DIR/docs"

echo "  Installing dependencies..."
npm install --silent 2>/dev/null

# Patch download URL placeholders (temporary, reverted after build)
echo "  Patching download URLs..."
DOCS_FILES=(
    docs/download.mdx
    docs/mutiauk.mdx
    docs/muti-metroo-manager.mdx
    docs/getting-started/installation.md
    docs/getting-started/quick-start.md
    docs/deployment/docker.md
)
for f in "${DOCS_FILES[@]}"; do
    if [ -f "$f" ]; then
        sed -i.bak \
            -e "s|__MUTI_METROO_RELEASE__|${MUTI_METROO_DOWNLOAD}|g" \
            -e "s|__MUTIAUK_RELEASE__|${MUTIAUK_DOWNLOAD}|g" \
            -e "s|__METROO_MANAGER_RELEASE__|${METROO_MANAGER_DOWNLOAD}|g" \
            "$f"
    fi
done

# Build (restore files even if build fails)
echo "  Building static site..."
BUILD_OK=true
npm run build || BUILD_OK=false

# Restore original files
for f in "${DOCS_FILES[@]}"; do
    if [ -f "$f.bak" ]; then
        mv "$f.bak" "$f"
    fi
done

if ! $BUILD_OK; then
    echo "Error: Build failed"
    exit 1
fi

# --- Deploy ---
echo ""
echo "=== Deploying to ${SERVER} ==="

# Upload to staging dir (writable by andris)
ssh "$SERVER" "rm -rf ${STAGING_DIR} && mkdir -p ${STAGING_DIR}"
rsync -az build/ "${SERVER}:${STAGING_DIR}/"

# Sync into place with sudo (preserves the directory inode for Docker bind mounts)
echo "  Syncing into ${REMOTE_DIR}..."
ssh "$SERVER" "sudo rsync -a --delete ${STAGING_DIR}/ ${REMOTE_DIR}/ && sudo chown -R www-data:www-data ${REMOTE_DIR} && rm -rf ${STAGING_DIR}"

echo ""
echo "=== Publish complete ==="
echo "Site: https://mutimetroo.com/"
