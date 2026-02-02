#!/bin/bash
#
# Echopoint CLI Installer
# https://github.com/nanostack-dev/echopoint-cli
#
# Usage:
#   curl -fsSL https://raw.githubusercontent.com/nanostack-dev/echopoint-cli/main/install.sh | bash
#
# Options:
#   -d, --dir DIR     Install directory (default: /usr/local/bin)
#   -v, --version VER Install specific version (default: latest)
#

set -e

REPO="nanostack-dev/echopoint-cli"
BINARY_NAME="echopoint"
INSTALL_DIR="/usr/local/bin"
VERSION=""

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

info() {
    echo -e "${GREEN}[INFO]${NC} $1"
}

warn() {
    echo -e "${YELLOW}[WARN]${NC} $1"
}

error() {
    echo -e "${RED}[ERROR]${NC} $1"
    exit 1
}

# Parse arguments
while [[ $# -gt 0 ]]; do
    case $1 in
        -d|--dir)
            INSTALL_DIR="$2"
            shift 2
            ;;
        -v|--version)
            VERSION="$2"
            shift 2
            ;;
        *)
            error "Unknown option: $1"
            ;;
    esac
done

# Detect OS and architecture
detect_platform() {
    OS=$(uname -s | tr '[:upper:]' '[:lower:]')
    ARCH=$(uname -m)

    case "$OS" in
        linux)
            OS="linux"
            ;;
        darwin)
            OS="darwin"
            ;;
        mingw*|msys*|cygwin*)
            OS="windows"
            ;;
        *)
            error "Unsupported operating system: $OS"
            ;;
    esac

    case "$ARCH" in
        x86_64|amd64)
            ARCH="amd64"
            ;;
        aarch64|arm64)
            ARCH="arm64"
            ;;
        *)
            error "Unsupported architecture: $ARCH"
            ;;
    esac

    # Windows on ARM64 not supported
    if [ "$OS" = "windows" ] && [ "$ARCH" = "arm64" ]; then
        error "Windows ARM64 is not supported"
    fi

    PLATFORM="${OS}_${ARCH}"
    info "Detected platform: $PLATFORM"
}

# Get latest version from GitHub
get_latest_version() {
    if [ -n "$VERSION" ]; then
        info "Using specified version: $VERSION"
        return
    fi

    info "Fetching latest version..."
    VERSION=$(curl -fsSL "https://api.github.com/repos/${REPO}/releases/latest" | grep '"tag_name":' | sed -E 's/.*"([^"]+)".*/\1/')
    
    if [ -z "$VERSION" ]; then
        error "Failed to get latest version"
    fi
    
    info "Latest version: $VERSION"
}

# Download and install
install() {
    # Remove 'v' prefix for archive name
    VERSION_NUM="${VERSION#v}"
    
    ARCHIVE_NAME="${BINARY_NAME}_${VERSION_NUM}_${PLATFORM}"
    
    if [ "$OS" = "windows" ]; then
        ARCHIVE_EXT="zip"
        BINARY_EXT=".exe"
    else
        ARCHIVE_EXT="tar.gz"
        BINARY_EXT=""
    fi

    DOWNLOAD_URL="https://github.com/${REPO}/releases/download/${VERSION}/${ARCHIVE_NAME}.${ARCHIVE_EXT}"
    
    info "Downloading from: $DOWNLOAD_URL"
    
    TMP_DIR=$(mktemp -d)
    trap "rm -rf $TMP_DIR" EXIT
    
    ARCHIVE_PATH="${TMP_DIR}/${ARCHIVE_NAME}.${ARCHIVE_EXT}"
    
    if ! curl -fsSL -o "$ARCHIVE_PATH" "$DOWNLOAD_URL"; then
        error "Failed to download archive"
    fi
    
    info "Extracting archive..."
    
    if [ "$ARCHIVE_EXT" = "zip" ]; then
        unzip -q "$ARCHIVE_PATH" -d "$TMP_DIR"
    else
        tar -xzf "$ARCHIVE_PATH" -C "$TMP_DIR"
    fi
    
    BINARY_PATH="${TMP_DIR}/${BINARY_NAME}${BINARY_EXT}"
    
    if [ ! -f "$BINARY_PATH" ]; then
        error "Binary not found in archive"
    fi
    
    info "Installing to $INSTALL_DIR..."
    
    # Create install directory if it doesn't exist
    if [ ! -d "$INSTALL_DIR" ]; then
        if ! mkdir -p "$INSTALL_DIR" 2>/dev/null; then
            warn "Cannot create $INSTALL_DIR, trying with sudo..."
            sudo mkdir -p "$INSTALL_DIR"
        fi
    fi
    
    # Install binary
    if ! mv "$BINARY_PATH" "${INSTALL_DIR}/${BINARY_NAME}${BINARY_EXT}" 2>/dev/null; then
        warn "Cannot install to $INSTALL_DIR, trying with sudo..."
        sudo mv "$BINARY_PATH" "${INSTALL_DIR}/${BINARY_NAME}${BINARY_EXT}"
    fi
    
    # Make executable
    chmod +x "${INSTALL_DIR}/${BINARY_NAME}${BINARY_EXT}" 2>/dev/null || \
        sudo chmod +x "${INSTALL_DIR}/${BINARY_NAME}${BINARY_EXT}"
    
    info "Installation complete!"
    echo ""
    echo "Run 'echopoint --help' to get started"
    echo ""
    
    # Verify installation
    if command -v echopoint &> /dev/null; then
        info "Installed version: $(echopoint version 2>/dev/null || echo 'unknown')"
    else
        warn "echopoint not found in PATH. You may need to add $INSTALL_DIR to your PATH"
        echo ""
        echo "Add this to your shell profile (~/.bashrc, ~/.zshrc, etc.):"
        echo "  export PATH=\"\$PATH:$INSTALL_DIR\""
    fi
}

main() {
    echo ""
    echo "  Echopoint CLI Installer"
    echo "  ========================"
    echo ""
    
    detect_platform
    get_latest_version
    install
}

main
