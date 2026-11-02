// Package flowdsl compiles a flow file into the CreateFlowRequest JSON the
// Echopoint API already accepts, and renders a stored flow back into that file.
// The file is YAML: nodes are named, and next and on_failure replace the edges
// array. Nothing here calls the API.
package flowdsl
