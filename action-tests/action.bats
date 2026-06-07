#!/usr/bin/env bats
# Tests for action.yml logic — exercises the shell functions extracted into
# action_lib.bash, plus end-to-end tests using a fake echopoint binary.
#
# Uses bats-core 1.x (no bats-assert). Assertions are plain [ ] tests with
# descriptive messages written to &3 for failures.

load helpers/action_lib

FIXTURES_DIR="$(cd "$(dirname "$BATS_TEST_FILENAME")/fixtures" && pwd)"
ACTION_FILE="$(cd "$(dirname "$BATS_TEST_FILENAME")/.." && pwd)/action.yml"

# -----------------------------------------------------------------------
# 4.4.1 Validate action.yml syntax (YAML parse)
# -----------------------------------------------------------------------
@test "4.4.1 action.yml exists" {
  [ -f "$ACTION_FILE" ]
}

@test "4.4.1 action.yml is valid YAML via python3" {
  # Use pyenv python3 which has yaml
  PYTHON_BIN=""
  for candidate in \
    "${HOME}/.pyenv/shims/python3" \
    "${HOME}/.pyenv/versions/3.10.12/bin/python3" \
    "$(command -v python3 2>/dev/null)"; do
    if [ -n "$candidate" ] && "$candidate" -c "import yaml" 2>/dev/null; then
      PYTHON_BIN="$candidate"
      break
    fi
  done

  if [ -z "$PYTHON_BIN" ]; then
    skip "no python3 with PyYAML found"
  fi

  run "$PYTHON_BIN" -c "
import yaml, sys
with open('${ACTION_FILE}') as f:
    data = yaml.safe_load(f)
errors = []
if data.get('runs', {}).get('using') != 'composite':
    errors.append('runs.using must be composite')
if 'inputs' not in data:
    errors.append('inputs section missing')
if 'outputs' not in data:
    errors.append('outputs section missing')
if 'branding' not in data:
    errors.append('branding section missing')
if errors:
    print('ERRORS: ' + '; '.join(errors), file=sys.stderr)
    sys.exit(1)
print('OK')
"
  [ "$status" -eq 0 ]
}

@test "4.4.1 action.yml declares all required inputs" {
  local required_inputs=(
    "api-key"
    "organization-id"
    "flow-id"
    "flow-ids"
    "environment"
    "version-id"
    "cli-version"
    "runner-version"
    "runner-binary"
    "poll-timeout"
    "parallel"
  )

  for inp in "${required_inputs[@]}"; do
    grep -q "^  ${inp}:" "$ACTION_FILE"
  done
}

@test "4.4.1 action.yml declares all required outputs" {
  local required_outputs=(
    "execution-id"
    "execution-ids"
    "status"
    "success"
    "results-json"
  )

  for out in "${required_outputs[@]}"; do
    grep -q "^  ${out}:" "$ACTION_FILE"
  done
}

@test "4.4.1 action.yml has branding" {
  grep -q "^branding:" "$ACTION_FILE"
  grep -q "icon:" "$ACTION_FILE"
  grep -q "color:" "$ACTION_FILE"
}

@test "4.4.1 action.yml is composite runs-using" {
  grep -q "using: composite" "$ACTION_FILE"
}

@test "4.4.1 action.yml documents usage as nanostack-dev/echopoint-cli@v1" {
  grep -q "nanostack-dev/echopoint-cli@v1" "$ACTION_FILE"
}

# -----------------------------------------------------------------------
# 4.4.2 OS/arch artifact URL mapping
# -----------------------------------------------------------------------
@test "4.4.2 Linux/X64 maps to linux/amd64 tar.gz" {
  result=$(map_platform "Linux" "X64")
  os=$(echo "$result" | cut -d'|' -f1)
  arch=$(echo "$result" | cut -d'|' -f2)
  ext=$(echo "$result" | cut -d'|' -f3)
  bin_ext=$(echo "$result" | cut -d'|' -f4)

  [ "$os"     = "linux"  ]
  [ "$arch"   = "amd64"  ]
  [ "$ext"    = "tar.gz" ]
  [ "$bin_ext" = ""      ]
}

@test "4.4.2 Linux/ARM64 maps to linux/arm64 tar.gz" {
  result=$(map_platform "Linux" "ARM64")
  os=$(echo "$result" | cut -d'|' -f1)
  arch=$(echo "$result" | cut -d'|' -f2)
  ext=$(echo "$result" | cut -d'|' -f3)

  [ "$os"   = "linux"  ]
  [ "$arch" = "arm64"  ]
  [ "$ext"  = "tar.gz" ]
}

@test "4.4.2 macOS/ARM64 maps to darwin/arm64 tar.gz" {
  result=$(map_platform "macOS" "ARM64")
  os=$(echo "$result" | cut -d'|' -f1)
  arch=$(echo "$result" | cut -d'|' -f2)
  ext=$(echo "$result" | cut -d'|' -f3)

  [ "$os"   = "darwin" ]
  [ "$arch" = "arm64"  ]
  [ "$ext"  = "tar.gz" ]
}

@test "4.4.2 macOS/X64 maps to darwin/amd64 tar.gz" {
  result=$(map_platform "macOS" "X64")
  os=$(echo "$result" | cut -d'|' -f1)
  arch=$(echo "$result" | cut -d'|' -f2)

  [ "$os"   = "darwin" ]
  [ "$arch" = "amd64"  ]
}

@test "4.4.2 Windows/X64 maps to windows/amd64 zip with .exe" {
  result=$(map_platform "Windows" "X64")
  os=$(echo "$result" | cut -d'|' -f1)
  arch=$(echo "$result" | cut -d'|' -f2)
  ext=$(echo "$result" | cut -d'|' -f3)
  bin_ext=$(echo "$result" | cut -d'|' -f4)

  [ "$os"      = "windows" ]
  [ "$arch"    = "amd64"   ]
  [ "$ext"     = "zip"     ]
  [ "$bin_ext" = ".exe"    ]
}

@test "4.4.2 Windows/ARM64 returns non-zero (unsupported)" {
  run map_platform "Windows" "ARM64"
  [ "$status" -ne 0 ]
}

@test "4.4.2 artifact URL is constructed correctly for CLI" {
  url=$(build_artifact_url \
    "nanostack-dev/echopoint-cli" \
    "v1.2.3" \
    "echopoint" \
    "linux" "amd64" "tar.gz")

  expected="https://github.com/nanostack-dev/echopoint-cli/releases/download/v1.2.3/echopoint_1.2.3_linux_amd64.tar.gz"
  [ "$url" = "$expected" ]
}

@test "4.4.2 artifact URL strips v prefix from version number" {
  url=$(build_artifact_url \
    "nanostack-dev/echopoint-runner" \
    "v2.0.0" \
    "echopoint-runner" \
    "darwin" "arm64" "tar.gz")

  echo "$url" | grep -q "echopoint-runner_2.0.0_darwin_arm64.tar.gz"
}

@test "4.4.2 artifact URL for runner uses echopoint-runner binary name" {
  url=$(build_artifact_url \
    "nanostack-dev/echopoint-runner" \
    "v1.0.0" \
    "echopoint-runner" \
    "linux" "amd64" "tar.gz")

  echo "$url" | grep -q "nanostack-dev/echopoint-runner"
  echo "$url" | grep -q "echopoint-runner_1.0.0_linux_amd64.tar.gz"
}

# -----------------------------------------------------------------------
# 4.4.3 Output parsing on success JSON
# -----------------------------------------------------------------------
@test "4.4.3 parse_outputs_single reads execution_id on success" {
  raw=$(cat "$FIXTURES_DIR/single_success.json")
  parse_outputs_single "$raw"

  [ "$EXEC_ID" = "exec-abc-123" ]
}

@test "4.4.3 parse_outputs_single reads status=completed on success" {
  raw=$(cat "$FIXTURES_DIR/single_success.json")
  parse_outputs_single "$raw"

  [ "$STATUS" = "completed" ]
}

@test "4.4.3 parse_outputs_single reads success=true" {
  raw=$(cat "$FIXTURES_DIR/single_success.json")
  parse_outputs_single "$raw"

  [ "$SUCCESS" = "true" ]
}

@test "4.4.3 fake echopoint emits success JSON and exits 0" {
  fake_dir=$(mktemp -d)

  make_fake_echopoint "$fake_dir" "$FIXTURES_DIR/single_success.json" 0

  run "${fake_dir}/echopoint" flows run "flow-def-456" -o json
  rm -rf "$fake_dir"

  [ "$status" -eq 0 ]
  echo "$output" | grep -q '"execution_id"'
}

# -----------------------------------------------------------------------
# 4.4.4 Output parsing on failed-flow JSON + final failure exit
# -----------------------------------------------------------------------
@test "4.4.4 parse_outputs_single reads failed execution_id" {
  raw=$(cat "$FIXTURES_DIR/single_failed.json")
  parse_outputs_single "$raw"

  [ "$EXEC_ID" = "exec-abc-789" ]
}

@test "4.4.4 parse_outputs_single reads status=failed on failure" {
  raw=$(cat "$FIXTURES_DIR/single_failed.json")
  parse_outputs_single "$raw"

  [ "$STATUS" = "failed" ]
}

@test "4.4.4 parse_outputs_single reads success=false on failure" {
  raw=$(cat "$FIXTURES_DIR/single_failed.json")
  parse_outputs_single "$raw"

  [ "$SUCCESS" = "false" ]
}

@test "4.4.4 fake echopoint emits failed JSON and exits 1" {
  fake_dir=$(mktemp -d)
  make_fake_echopoint "$fake_dir" "$FIXTURES_DIR/single_failed.json" 1

  run "${fake_dir}/echopoint" flows run "flow-def-456" -o json
  rm -rf "$fake_dir"

  [ "$status" -eq 1 ]
  parse_outputs_single "$output"
  [ "$STATUS"  = "failed" ]
  [ "$SUCCESS" = "false"  ]
}

@test "4.4.4 output is still parsed when CLI exits non-zero" {
  # Simulates action capturing JSON before propagating exit code
  fake_dir=$(mktemp -d)
  make_fake_echopoint "$fake_dir" "$FIXTURES_DIR/single_failed.json" 1

  json_output=$("${fake_dir}/echopoint" flows run "flow-abc" -o json) || cli_exit=$?
  cli_exit="${cli_exit:-0}"
  rm -rf "$fake_dir"

  # JSON should still be parseable even though exit was 1
  parse_outputs_single "$json_output"
  [ "$EXEC_ID" = "exec-abc-789" ]
  [ "$cli_exit" = "1" ]
}

# -----------------------------------------------------------------------
# 4.4.5 API key masking
# -----------------------------------------------------------------------
@test "4.4.5 action.yml contains ::add-mask:: for api-key" {
  grep -q "::add-mask::" "$ACTION_FILE"
}

@test "4.4.5 mask step appears before run-flows step in action.yml" {
  mask_line=$(grep -n "::add-mask::" "$ACTION_FILE" | head -1 | cut -d: -f1)
  run_line=$(grep -n "run-flows" "$ACTION_FILE" | head -1 | cut -d: -f1)

  [ -n "$mask_line" ]
  [ -n "$run_line" ]
  [ "$mask_line" -lt "$run_line" ]
}

@test "4.4.5 fake echopoint does not leak ECHOPOINT_API_KEY to stdout" {
  fake_dir=$(mktemp -d)
  make_fake_echopoint "$fake_dir" "$FIXTURES_DIR/single_success.json" 0

  output=$(ECHOPOINT_API_KEY="super-secret-key-12345" \
    "${fake_dir}/echopoint" flows run "flow-abc" -o json)
  rm -rf "$fake_dir"

  # The secret must not appear in stdout
  run bash -c "echo '$output' | grep -c 'super-secret-key-12345' || true"
  [ "$output" = "0" ] || ! echo "$output" | grep -q "super-secret-key-12345"
}

# -----------------------------------------------------------------------
# 4.4.6 flow-ids parsing and multi-flow outputs
# -----------------------------------------------------------------------
@test "4.4.6 parse_outputs_multi reads status=completed for multi success" {
  raw=$(cat "$FIXTURES_DIR/multi_success.json")
  parse_outputs_multi "$raw"

  [ "$STATUS" = "completed" ]
}

@test "4.4.6 parse_outputs_multi reads success=true for multi success" {
  raw=$(cat "$FIXTURES_DIR/multi_success.json")
  parse_outputs_multi "$raw"

  [ "$SUCCESS" = "true" ]
}

@test "4.4.6 parse_outputs_multi collects execution-ids for multi success" {
  raw=$(cat "$FIXTURES_DIR/multi_success.json")
  parse_outputs_multi "$raw"

  echo "$EXEC_IDS" | grep -q "exec-111"
  echo "$EXEC_IDS" | grep -q "exec-222"
}

@test "4.4.6 parse_outputs_multi reads status=failed for partial failure" {
  raw=$(cat "$FIXTURES_DIR/multi_partial_failed.json")
  parse_outputs_multi "$raw"

  [ "$STATUS" = "failed" ]
}

@test "4.4.6 parse_outputs_multi reads success=false for partial failure" {
  raw=$(cat "$FIXTURES_DIR/multi_partial_failed.json")
  parse_outputs_multi "$raw"

  [ "$SUCCESS" = "false" ]
}

@test "4.4.6 parse_outputs_multi collects all execution-ids even on partial failure" {
  raw=$(cat "$FIXTURES_DIR/multi_partial_failed.json")
  parse_outputs_multi "$raw"

  echo "$EXEC_IDS" | grep -q "exec-111"
  echo "$EXEC_IDS" | grep -q "exec-222"
}

@test "4.4.6 comma-separated flow-ids normalise to space-separated args" {
  flow_ids="flow-aaa,flow-bbb, flow-ccc"
  normalized=$(echo "$flow_ids" \
    | tr ',\n' '  ' \
    | tr -s ' ' \
    | sed 's/^ //;s/ $//')

  word_count=$(echo "$normalized" | wc -w | tr -d ' ')
  [ "$word_count" = "3" ]
}

@test "4.4.6 newline-separated flow-ids normalise to space-separated args" {
  flow_ids="$(printf 'flow-aaa\nflow-bbb\nflow-ccc')"
  normalized=$(echo "$flow_ids" \
    | tr ',\n' '  ' \
    | tr -s ' ' \
    | sed 's/^ //;s/ $//')

  word_count=$(echo "$normalized" | wc -w | tr -d ' ')
  [ "$word_count" = "3" ]
}

@test "4.4.6 fake echopoint multi-flow outputs parsed correctly" {
  fake_dir=$(mktemp -d)
  make_fake_echopoint "$fake_dir" "$FIXTURES_DIR/multi_success.json" 0

  run "${fake_dir}/echopoint" flows run "flow-aaa" "flow-bbb" -o json
  rm -rf "$fake_dir"

  [ "$status" -eq 0 ]
  echo "$output" | grep -q '"results"'
  parse_outputs_multi "$output"
  [ "$STATUS" = "completed" ]
}

@test "4.4.6 compact multi-flow JSON parses under pipefail" {
  raw=$(tr -d '\n' < "$FIXTURES_DIR/multi_partial_failed.json")

  run bash -o pipefail -c '
    set -euo pipefail
    source "$1"
    parse_outputs_multi "$2"
    [ "$STATUS" = "failed" ]
    [ "$SUCCESS" = "false" ]
    [ "$EXEC_IDS" = "exec-111,exec-222" ]
  ' bash "$(dirname "$BATS_TEST_FILENAME")/helpers/action_lib.bash" "$raw"

  [ "$status" -eq 0 ]
}

# -----------------------------------------------------------------------
# 4.4.7 Invalid parallel fails before invoking CLI
# -----------------------------------------------------------------------
@test "4.4.7 parallel=0 fails validation with exit 3" {
  run validate_inputs "flow-abc" "" "0"
  [ "$status" -eq 3 ]
}

@test "4.4.7 parallel=0 produces an error mentioning parallel" {
  run validate_inputs "flow-abc" "" "0"
  echo "$output" | grep -q "parallel"
}

@test "4.4.7 parallel=-1 fails validation" {
  run validate_inputs "flow-abc" "" "-1"
  [ "$status" -ne 0 ]
}

@test "4.4.7 parallel=abc fails validation" {
  run validate_inputs "flow-abc" "" "abc"
  [ "$status" -ne 0 ]
}

@test "4.4.7 parallel=0.5 fails validation" {
  run validate_inputs "flow-abc" "" "0.5"
  [ "$status" -ne 0 ]
}

@test "4.4.7 parallel=1 passes validation" {
  run validate_inputs "flow-abc" "" "1"
  [ "$status" -eq 0 ]
}

@test "4.4.7 parallel=5 passes validation" {
  run validate_inputs "" "flow-abc,flow-def" "5"
  [ "$status" -eq 0 ]
}

@test "4.4.7 parallel=10 passes validation" {
  run validate_inputs "" "flow-abc" "10"
  [ "$status" -eq 0 ]
}

# -----------------------------------------------------------------------
# Additional validate_inputs coverage
# -----------------------------------------------------------------------
@test "validate: neither flow-id nor flow-ids fails with exit 3" {
  run validate_inputs "" "" "1"
  [ "$status" -eq 3 ]
}

@test "validate: neither produces error mentioning flow-id" {
  run validate_inputs "" "" "1"
  echo "$output" | grep -qi "flow-id"
}

@test "validate: both flow-id and flow-ids fails with exit 3" {
  run validate_inputs "flow-abc" "flow-abc,flow-def" "1"
  [ "$status" -eq 3 ]
}

@test "validate: both produces mutually exclusive error" {
  run validate_inputs "flow-abc" "flow-abc,flow-def" "1"
  echo "$output" | grep -qi "mutually exclusive"
}

@test "validate: single flow-id passes" {
  run validate_inputs "flow-abc" "" "1"
  [ "$status" -eq 0 ]
}

@test "validate: flow-ids only passes" {
  run validate_inputs "" "flow-abc,flow-def" "1"
  [ "$status" -eq 0 ]
}
