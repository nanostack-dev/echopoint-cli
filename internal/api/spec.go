package api

import _ "embed"

// OpenAPISpec is the stripped prod contract embedded at build time. Commands read
// request schemas from it. Refresh it via the same sync flow that
// regenerates client.gen.go (see docs/environment-management.md).
//
//go:embed openapi.yaml
var OpenAPISpec []byte
