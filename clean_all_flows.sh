#!/bin/bash

# Clean All Flows Script
# Deletes all existing flows from Echopoint

set -e

ECHOPOINT="./dist/echopoint-darwin-arm64"

# Authenticate
$ECHOPOINT auth login --local

# List all flows and delete them
$ECHOPOINT flows list -o json | jq -r '.items[].id' | while read -r FLOW_ID; do
    $ECHOPOINT flows delete "$FLOW_ID"
done

echo "✅ All flows deleted"
