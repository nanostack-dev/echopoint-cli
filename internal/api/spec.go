package api

import _ "embed"

// OpenAPISpec is the stripped prod contract embedded at build time. It is the
// single source of truth for the MCP tool catalog: operations annotated with
// `x-ai-tool: true` become MCP tools. Refresh it via the same sync flow that
// regenerates client.gen.go (see docs/environment-management.md).
//
//go:embed openapi.yaml
var OpenAPISpec []byte
