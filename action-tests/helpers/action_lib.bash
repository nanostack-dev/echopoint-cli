#!/usr/bin/env bash
# Shared helpers for action tests

# --- Environment simulation ---

# Set up a minimal fake GitHub Actions environment
setup_github_env() {
  export RUNNER_OS="${RUNNER_OS:-Linux}"
  export RUNNER_ARCH="${RUNNER_ARCH:-X64}"
  export RUNNER_TEMP="${BATS_TMPDIR}/runner_temp"
  export GITHUB_PATH="${BATS_TMPDIR}/github_path"
  export GITHUB_OUTPUT="${BATS_TMPDIR}/github_output"
  export GITHUB_STEP_SUMMARY="${BATS_TMPDIR}/github_step_summary"
  mkdir -p "$RUNNER_TEMP"
  touch "$GITHUB_PATH" "$GITHUB_OUTPUT" "$GITHUB_STEP_SUMMARY"
}

teardown_github_env() {
  rm -f "$GITHUB_OUTPUT" "$GITHUB_STEP_SUMMARY" "$GITHUB_PATH"
  rm -rf "$RUNNER_TEMP"
}

# Read a value from simulated GITHUB_OUTPUT
get_output() {
  local key="$1"
  # Handle multiline values delimited by heredoc-style EOF markers
  # For simple scalar values, grep+sed works fine
  grep "^${key}=" "$GITHUB_OUTPUT" | tail -1 | sed "s/^${key}=//"
}

# --- Fake echopoint binary factory ---

# Create a fake `echopoint` binary that accepts `flows run` and emits
# canned JSON + a given exit code.
# Usage: make_fake_echopoint <dir> <json_fixture_file> <exit_code>
make_fake_echopoint() {
  local dir="$1"
  local json_file="$2"
  local exit_code="${3:-0}"

  mkdir -p "$dir"
  cat > "${dir}/echopoint" <<FAKE_EOF
#!/usr/bin/env bash
# Fake echopoint binary for action tests
if [[ "\$1" == "flows" && "\$2" == "run" ]]; then
  cat "${json_file}"
  exit ${exit_code}
fi
exit 3
FAKE_EOF
  chmod +x "${dir}/echopoint"
}

# --- Helpers to exercise action logic inline (no GitHub runner needed) ---

# Source the validation + platform logic extracted into testable shell functions.
# These mirror the logic in action.yml so we can unit-test it without a runner.

validate_inputs() {
  local flow_id="$1"
  local flow_ids="$2"
  local parallel="${3:-1}"

  if [ -z "$flow_id" ] && [ -z "$flow_ids" ]; then
    echo "error: Either 'flow-id' or 'flow-ids' must be provided."
    return 3
  fi
  if [ -n "$flow_id" ] && [ -n "$flow_ids" ]; then
    echo "error: 'flow-id' and 'flow-ids' are mutually exclusive."
    return 3
  fi
  if ! [[ "$parallel" =~ ^[0-9]+$ ]] || [ "$parallel" -lt 1 ]; then
    echo "error: 'parallel' must be an integer >= 1, got: $parallel"
    return 3
  fi
  return 0
}

map_platform() {
  local os="$1"
  local arch="$2"

  local os_name arch_name archive_ext binary_ext

  case "$os" in
    Linux)   os_name="linux" ;;
    macOS)   os_name="darwin" ;;
    Windows) os_name="windows" ;;
    *)
      echo "unsupported-os"
      return 1
      ;;
  esac

  case "$arch" in
    X64)   arch_name="amd64" ;;
    ARM64) arch_name="arm64" ;;
    *)
      echo "unsupported-arch"
      return 1
      ;;
  esac

  if [ "$os_name" = "windows" ] && [ "$arch_name" = "arm64" ]; then
    echo "unsupported-windows-arm64"
    return 1
  fi

  if [ "$os_name" = "windows" ]; then
    archive_ext="zip"
    binary_ext=".exe"
  else
    archive_ext="tar.gz"
    binary_ext=""
  fi

  echo "${os_name}|${arch_name}|${archive_ext}|${binary_ext}"
  return 0
}

build_artifact_url() {
  local repo="$1"
  local version="$2"
  local binary_name="$3"
  local os_name="$4"
  local arch_name="$5"
  local archive_ext="$6"

  local version_num="${version#v}"
  echo "https://github.com/${repo}/releases/download/${version}/${binary_name}_${version_num}_${os_name}_${arch_name}.${archive_ext}"
}

parse_outputs_single() {
  local raw="$1"

  EXEC_ID=$(echo "$raw" | grep -o '"execution_id": *"[^"]*"' | head -1 | sed 's/"execution_id": *"//;s/"//')
  STATUS=$(echo "$raw"  | grep -o '"status": *"[^"]*"'       | head -1 | sed 's/"status": *"//;s/"//')
  SUCCESS=$(echo "$raw" | grep -o '"success": *[^,}]*'       | head -1 | sed 's/"success": *//;s/[[:space:]]//g')
}

parse_outputs_multi() {
  local raw="$1"

  STATUS=$(echo "$raw"   | grep -o '"status": *"[^"]*"' | head -1 | sed 's/"status": *"//;s/"//')
  SUCCESS=$(echo "$raw"  | grep -o '"success": *[^,}]*' | head -1 | sed 's/"success": *//;s/[[:space:]]//g')
  EXEC_IDS=$(echo "$raw" \
    | grep -o '"execution_id": *"[^"]*"' \
    | sed 's/"execution_id": *"//;s/"//' \
    | tr '\n' ',' | sed 's/,$//')
}
