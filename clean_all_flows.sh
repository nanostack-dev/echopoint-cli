#!/bin/bash

# Clean All Flows Script
# Deletes all existing flows from Echopoint

set -e

ECHOPOINT="./dist/echopoint-darwin-arm64"

# Authenticate
# A local API (make dev) signs in with the dev Clerk instance.
export ECHOPOINT_API_URL="${ECHOPOINT_API_URL:-http://localhost:8080}"
$ECHOPOINT auth login

# List all flows and delete them
$ECHOPOINT flow list -o json | jq -r '.items[].id' | while read -r FLOW_ID; do
    $ECHOPOINT flow delete --yes "$FLOW_ID"
done

echo "✅ All flows deleted"
