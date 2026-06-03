#!/bin/sh
#
# Echopoint CLI Installer (macOS / Linux)
# https://github.com/nanostack-dev/echopoint-cli
#
# Usage:
#   curl -fsSL https://raw.githubusercontent.com/nanostack-dev/echopoint-cli/main/install.sh | sh
#
# Options (pass after `-s --` when piping, e.g. `... | sh -s -- --dir ~/bin`):
#   -d, --dir DIR        Install directory (default: $XDG_BIN_HOME, else ~/.local/bin)
#   -v, --version VER    Install a specific version (default: latest)
#       --no-modify-path Do not add the install dir to your shell PATH
#
# Installs into your home directory by default — no sudo required.

set -eu

REPO="nanostack-dev/echopoint-cli"
BINARY_NAME="echopoint"
VERSION=""
INSTALL_DIR=""
MODIFY_PATH=1

# Colors (disabled when not a TTY)
if [ -t 1 ]; then
  RED='\033[0;31m'; GREEN='\033[0;32m'; YELLOW='\033[1;33m'; NC='\033[0m'
else
  RED=''; GREEN=''; YELLOW=''; NC=''
fi

info()  { printf '%b[INFO]%b %s\n'  "$GREEN"  "$NC" "$1"; }
warn()  { printf '%b[WARN]%b %s\n'  "$YELLOW" "$NC" "$1" >&2; }
error() { printf '%b[ERROR]%b %s\n' "$RED"    "$NC" "$1" >&2; exit 1; }

# Parse arguments
while [ $# -gt 0 ]; do
  case "$1" in
    -d|--dir)         INSTALL_DIR="$2"; shift 2 ;;
    -v|--version)     VERSION="$2"; shift 2 ;;
    --no-modify-path) MODIFY_PATH=0; shift ;;
    *) error "Unknown option: $1" ;;
  esac
done

# Default install dir: honor XDG, fall back to ~/.local/bin (no sudo needed).
if [ -z "$INSTALL_DIR" ]; then
  if [ -n "${XDG_BIN_HOME:-}" ]; then
    INSTALL_DIR="$XDG_BIN_HOME"
  elif [ -n "${XDG_DATA_HOME:-}" ]; then
    INSTALL_DIR="$XDG_DATA_HOME/../bin"
  else
    INSTALL_DIR="$HOME/.local/bin"
  fi
fi

need_cmd() { command -v "$1" >/dev/null 2>&1 || error "required command not found: $1"; }
need_cmd curl
need_cmd tar

detect_platform() {
  OS=$(uname -s | tr '[:upper:]' '[:lower:]')
  ARCH=$(uname -m)
  case "$OS" in
    linux)  OS="linux" ;;
    darwin) OS="darwin" ;;
    *) error "Unsupported operating system: $OS (use the Windows installer install.ps1 on Windows)" ;;
  esac
  case "$ARCH" in
    x86_64|amd64)   ARCH="amd64" ;;
    aarch64|arm64)  ARCH="arm64" ;;
    *) error "Unsupported architecture: $ARCH" ;;
  esac
  PLATFORM="${OS}_${ARCH}"
  info "Detected platform: $PLATFORM"
}

get_latest_version() {
  if [ -n "$VERSION" ]; then
    info "Using specified version: $VERSION"
    return
  fi
  info "Fetching latest version..."
  VERSION=$(curl -fsSL "https://api.github.com/repos/${REPO}/releases/latest" \
    | grep '"tag_name":' | sed -E 's/.*"([^"]+)".*/\1/')
  [ -n "$VERSION" ] || error "Failed to determine latest version"
  info "Latest version: $VERSION"
}

# sha256_of FILE -> prints hex digest
sha256_of() {
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$1" | awk '{print $1}'
  elif command -v shasum >/dev/null 2>&1; then
    shasum -a 256 "$1" | awk '{print $1}'
  else
    echo ""
  fi
}

verify_checksum() {
  archive_path="$1"; archive_name="$2"; checksums_path="$3"
  want=$(grep -E "  ${archive_name}\$" "$checksums_path" | awk '{print $1}' | head -n1)
  [ -n "$want" ] || error "no checksum listed for $archive_name"
  got=$(sha256_of "$archive_path")
  if [ -z "$got" ]; then
    warn "no sha256 tool found; skipping checksum verification"
    return
  fi
  if [ "$got" != "$want" ]; then
    error "checksum mismatch for $archive_name (got $got, want $want)"
  fi
  info "Checksum verified."
}

install_bin() {
  VERSION_NUM="${VERSION#v}"
  ARCHIVE_NAME="${BINARY_NAME}_${VERSION_NUM}_${PLATFORM}.tar.gz"
  BASE="https://github.com/${REPO}/releases/download/${VERSION}"

  TMP_DIR=$(mktemp -d)
  trap 'rm -rf "$TMP_DIR"' EXIT

  info "Downloading ${ARCHIVE_NAME}..."
  curl -fsSL -o "$TMP_DIR/$ARCHIVE_NAME" "$BASE/$ARCHIVE_NAME" \
    || error "Failed to download $ARCHIVE_NAME"

  if curl -fsSL -o "$TMP_DIR/checksums.txt" "$BASE/checksums.txt" 2>/dev/null; then
    verify_checksum "$TMP_DIR/$ARCHIVE_NAME" "$ARCHIVE_NAME" "$TMP_DIR/checksums.txt"
  else
    warn "checksums.txt not found for $VERSION; skipping verification"
  fi

  info "Extracting..."
  tar -xzf "$TMP_DIR/$ARCHIVE_NAME" -C "$TMP_DIR"
  [ -f "$TMP_DIR/$BINARY_NAME" ] || error "binary not found in archive"

  mkdir -p "$INSTALL_DIR"
  mv "$TMP_DIR/$BINARY_NAME" "$INSTALL_DIR/$BINARY_NAME"
  chmod +x "$INSTALL_DIR/$BINARY_NAME"
  info "Installed to $INSTALL_DIR/$BINARY_NAME"
}

# Write ~/.echopoint/env and source it from the user's shell rc files so the
# install dir is on PATH in new shells. Mirrors the uv/rustup approach.
configure_path() {
  if [ "$MODIFY_PATH" -eq 0 ]; then
    return
  fi

  env_dir="$HOME/.echopoint"
  env_file="$env_dir/env"
  mkdir -p "$env_dir"
  cat > "$env_file" <<EOF
#!/bin/sh
# Added by the echopoint installer. Prepends the CLI install dir to PATH.
case ":\${PATH}:" in
  *:"$INSTALL_DIR":*) ;;
  *) export PATH="$INSTALL_DIR:\$PATH" ;;
esac
EOF

  source_line=". \"$env_file\""
  added=""
  for rc in "$HOME/.profile" "$HOME/.bashrc" "$HOME/.bash_profile" "$HOME/.zshrc" "$HOME/.zshenv"; do
    [ -e "$rc" ] || continue
    if ! grep -qsF "$env_file" "$rc"; then
      printf '\n# echopoint\n%s\n' "$source_line" >> "$rc"
      added="$added $rc"
    fi
  done

  # zsh: ensure at least .zshrc gets it even if none existed yet.
  if [ -n "${ZSH_VERSION:-}" ] || [ "$(basename "${SHELL:-}")" = "zsh" ]; then
    if [ ! -e "$HOME/.zshrc" ]; then
      printf '# echopoint\n%s\n' "$source_line" > "$HOME/.zshrc"
      added="$added $HOME/.zshrc"
    fi
  fi

  if [ -n "$added" ]; then
    info "Updated shell profile(s):$added"
    info "Run this now (or open a new terminal): $source_line"
  fi
}

on_path() {
  case ":${PATH}:" in
    *:"$INSTALL_DIR":*) return 0 ;;
    *) return 1 ;;
  esac
}

main() {
  printf '\n  Echopoint CLI Installer\n  =======================\n\n'
  detect_platform
  get_latest_version
  install_bin
  configure_path

  printf '\n'
  info "Installation complete!"
  if on_path; then
    info "Installed version: $("$INSTALL_DIR/$BINARY_NAME" version --short 2>/dev/null || echo unknown)"
    printf "\nRun 'echopoint --help' to get started.\n"
  else
    warn "$INSTALL_DIR is not on your PATH yet."
    if [ "$MODIFY_PATH" -eq 1 ]; then
      printf "\nOpen a new terminal, or run:\n  . \"%s/.echopoint/env\"\n" "$HOME"
    else
      printf "\nAdd it to your shell profile:\n  export PATH=\"%s:\$PATH\"\n" "$INSTALL_DIR"
    fi
  fi
  printf '\n'
}

main
