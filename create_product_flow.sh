#!/bin/bash

# Create Nanostack Product Lifecycle Flow using granular CLI commands
# Creates a complete CRUD flow step by step

set -e

ECHOPOINT="./dist/echopoint-darwin-arm64"

# Default values for environment variables (can be overridden)
EMAIL="${EMAIL:-admin@example.com}"
PASSWORD="${PASSWORD:-admin123}"
PRODUCT_NAME="${PRODUCT_NAME:-Test Product}"
PRODUCT_DESCRIPTION="${PRODUCT_DESCRIPTION:-A test product description}"
UPDATED_DESCRIPTION="${UPDATED_DESCRIPTION:-Updated product description}"

# Authenticate
$ECHOPOINT auth login --local

# Create empty flow
FLOW_ID=$($ECHOPOINT flows create-interactive --name "Nanostack Product Lifecycle" 2>&1 | grep "ID:" | awk '{print $2}')
echo "Created flow: $FLOW_ID"

# Node 1: Platform Login
NODE1=$($ECHOPOINT flows node add "$FLOW_ID" --type request --name "Platform Login" --method POST --url "https://apidev.nanostack.dev/v1/auth/login" --headers '{"Content-Type": "application/json"}' --body '{"email": "{{input.email}}", "password": "{{input.password}}"}' 2>&1 | grep -o 'Node added: [^[:space:]]*' | awk '{print $3}')
echo "Created node 1 (Login): $NODE1"

# Add output to Node 1
$ECHOPOINT flows node output add "$FLOW_ID" "$NODE1" --name "access_token" --extractor jsonPath --path "$.accessToken"
echo "Added output 'access_token' to node 1"

# Node 2: Create Product
NODE2=$($ECHOPOINT flows node add "$FLOW_ID" --type request --name "Create Product" --method POST --url "https://apidev.nanostack.dev/v1/products" --headers "{\"Content-Type\": \"application/json\", \"Authorization\": \"Bearer {{$NODE1.outputs.access_token}}\"}" --body '{"name": "{{input.product_name}}", "description": "{{input.product_description}}"}' 2>&1 | grep -o 'Node added: [^[:space:]]*' | awk '{print $3}')
echo "Created node 2 (Create Product): $NODE2"

# Add outputs to Node 2
$ECHOPOINT flows node output add "$FLOW_ID" "$NODE2" --name "product_id" --extractor jsonPath --path "$.id"
$ECHOPOINT flows node output add "$FLOW_ID" "$NODE2" --name "product_name" --extractor jsonPath --path "$.name"
echo "Added outputs to node 2"

# Add assertion to Node 2
$ECHOPOINT flows node assertion add "$FLOW_ID" "$NODE2" --extractor statusCode --operator equals --value "201"
echo "Added assertion to node 2"

# Node 3: Get Product
NODE3=$($ECHOPOINT flows node add "$FLOW_ID" --type request --name "Get Product" --method GET --url "https://apidev.nanostack.dev/v1/products/{{$NODE2.outputs.product_id}}" --headers "{\"Authorization\": \"Bearer {{$NODE1.outputs.access_token}}\"}" 2>&1 | grep -o 'Node added: [^[:space:]]*' | awk '{print $3}')
echo "Created node 3 (Get Product): $NODE3"

# Add output to Node 3
$ECHOPOINT flows node output add "$FLOW_ID" "$NODE3" --name "retrieved_product" --extractor body
echo "Added output to node 3"

# Add assertions to Node 3
$ECHOPOINT flows node assertion add "$FLOW_ID" "$NODE3" --extractor statusCode --operator equals --value "200"
$ECHOPOINT flows node assertion add "$FLOW_ID" "$NODE3" --extractor jsonPath --path "$.name" --operator equals --value "{{$NODE2.outputs.product_name}}"
echo "Added assertions to node 3"

# Node 4: Update Product
NODE4=$($ECHOPOINT flows node add "$FLOW_ID" --type request --name "Update Product" --method PATCH --url "https://apidev.nanostack.dev/v1/products/{{$NODE2.outputs.product_id}}" --headers "{\"Content-Type\": \"application/json\", \"Authorization\": \"Bearer {{$NODE1.outputs.access_token}}\"}" --body '{"description": "{{input.updated_description}}"}' 2>&1 | grep -o 'Node added: [^[:space:]]*' | awk '{print $3}')
echo "Created node 4 (Update Product): $NODE4"

# Add output to Node 4
$ECHOPOINT flows node output add "$FLOW_ID" "$NODE4" --name "updated_product" --extractor body
echo "Added output to node 4"

# Add assertion to Node 4
$ECHOPOINT flows node assertion add "$FLOW_ID" "$NODE4" --extractor statusCode --operator equals --value "200"
echo "Added assertion to node 4"

# Node 5: Verify Update
NODE5=$($ECHOPOINT flows node add "$FLOW_ID" --type request --name "Verify Update" --method GET --url "https://apidev.nanostack.dev/v1/products/{{$NODE2.outputs.product_id}}" --headers "{\"Authorization\": \"Bearer {{$NODE1.outputs.access_token}}\"}" 2>&1 | grep -o 'Node added: [^[:space:]]*' | awk '{print $3}')
echo "Created node 5 (Verify Update): $NODE5"

# Add assertions to Node 5
$ECHOPOINT flows node assertion add "$FLOW_ID" "$NODE5" --extractor statusCode --operator equals --value "200"
$ECHOPOINT flows node assertion add "$FLOW_ID" "$NODE5" --extractor jsonPath --path "$.description" --operator equals --value "{{input.updated_description}}"
echo "Added assertions to node 5"

# Node 6: Delete Product
NODE6=$($ECHOPOINT flows node add "$FLOW_ID" --type request --name "Delete Product" --method DELETE --url "https://apidev.nanostack.dev/v1/products/{{$NODE2.outputs.product_id}}" --headers "{\"Authorization\": \"Bearer {{$NODE1.outputs.access_token}}\"}" 2>&1 | grep -o 'Node added: [^[:space:]]*' | awk '{print $3}')
echo "Created node 6 (Delete Product): $NODE6"

# Add assertion to Node 6
$ECHOPOINT flows node assertion add "$FLOW_ID" "$NODE6" --extractor statusCode --operator equals --value "204"
echo "Added assertion to node 6"

# Node 7: Verify Delete
NODE7=$($ECHOPOINT flows node add "$FLOW_ID" --type request --name "Verify Delete" --method GET --url "https://apidev.nanostack.dev/v1/products/{{$NODE2.outputs.product_id}}" --headers "{\"Authorization\": \"Bearer {{$NODE1.outputs.access_token}}\"}" 2>&1 | grep -o 'Node added: [^[:space:]]*' | awk '{print $3}')
echo "Created node 7 (Verify Delete): $NODE7"

# Add assertion to Node 7
$ECHOPOINT flows node assertion add "$FLOW_ID" "$NODE7" --extractor statusCode --operator equals --value "404"
echo "Added assertion to node 7"

# Create edges (connect nodes in sequence)
$ECHOPOINT flows edge add "$FLOW_ID" --from "$NODE1" --to "$NODE2" --type success
echo "Created edge: $NODE1 -> $NODE2"

$ECHOPOINT flows edge add "$FLOW_ID" --from "$NODE2" --to "$NODE3" --type success
echo "Created edge: $NODE2 -> $NODE3"

$ECHOPOINT flows edge add "$FLOW_ID" --from "$NODE3" --to "$NODE4" --type success
echo "Created edge: $NODE3 -> $NODE4"

$ECHOPOINT flows edge add "$FLOW_ID" --from "$NODE4" --to "$NODE5" --type success
echo "Created edge: $NODE4 -> $NODE5"

$ECHOPOINT flows edge add "$FLOW_ID" --from "$NODE5" --to "$NODE6" --type success
echo "Created edge: $NODE5 -> $NODE6"

$ECHOPOINT flows edge add "$FLOW_ID" --from "$NODE6" --to "$NODE7" --type success
echo "Created edge: $NODE6 -> $NODE7"

# Set environment variables for the flow inputs
echo ""
echo "Setting environment variables..."
$ECHOPOINT flows env set "$FLOW_ID" \
  --var "email=$EMAIL" \
  --var "password=$PASSWORD" \
  --var "product_name=$PRODUCT_NAME" \
  --var "product_description=$PRODUCT_DESCRIPTION" \
  --var "updated_description=$UPDATED_DESCRIPTION"
echo "✓ Environment variables set"

echo ""
echo "✅ Flow created successfully!"
echo "Flow ID: $FLOW_ID"
echo ""
echo "Nodes: 7"
echo "Edges: 6"
echo "Environment variables: 5"
