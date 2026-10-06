package commands

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/google/uuid"
	"github.com/nanostack-dev/echopoint-kit/apispec"

	"echopoint-cli/internal/api"
	"echopoint-cli/internal/output"
)

// specPublished is the Live version an edit published, or found already
// published under the same command ID.
type specPublished struct {
	version  *api.SpecVersion
	replayed bool
}

// specRefusedError is an error answer of the API to an edit, with the message
// the API gave. The edit did not happen.
type specRefusedError struct {
	message string
}

func (e *specRefusedError) Error() string { return e.message }

// specEditOutcome is what an edit made: the edited document of a dry run, or
// the Live version it published.
type specEditOutcome struct {
	document  []byte
	published *specPublished
}

// specLiveTarget edits the Live version of a spec in EchoPoint: it publishes
// the next Live version with the commands, in one request.
type specLiveTarget struct {
	state     *AppState
	name      string
	commandID uuid.UUID
	pulled    *api.SpecDocument
}

// Document pulls the Live version, so a parameter is found in the document
// the commands apply to.
func (t *specLiveTarget) Document(ctx context.Context) ([]byte, error) {
	if t.pulled == nil {
		pulled, err := pullSpec(ctx, t.state, t.name, "")
		if err != nil {
			return nil, err
		}
		t.pulled = pulled
	}
	return []byte(t.pulled.Document), nil
}

func (t *specLiveTarget) Apply(ctx context.Context, commands []apispec.Command, dryRun bool) (specEditOutcome, error) {
	if dryRun {
		return t.applyLocally(ctx, commands)
	}
	request := struct {
		CommandID   uuid.UUID         `json:"command_id"`
		BaseVersion string            `json:"base_version,omitempty"`
		Commands    []apispec.Command `json:"commands"`
	}{CommandID: t.commandID, Commands: commands}
	if t.pulled != nil {
		request.BaseVersion = t.pulled.Version
	}
	body, err := json.Marshal(request)
	if err != nil {
		return specEditOutcome{}, err
	}
	resp, err := t.send(ctx, body)
	if err != nil {
		// A network error does not say whether the commands were applied. The
		// same command ID makes the second try safe: an applied request answers
		// with its version and applies nothing again.
		resp, err = t.send(ctx, body)
	}
	if err != nil {
		return specEditOutcome{}, err
	}
	switch {
	case resp.JSON201 != nil:
		return specEditOutcome{published: &specPublished{version: resp.JSON201}}, nil
	case resp.JSON200 != nil:
		return specEditOutcome{published: &specPublished{version: resp.JSON200, replayed: true}}, nil
	}
	return specEditOutcome{}, &specRefusedError{message: apiErrorMessage(resp.HTTPResponse, resp.Body)}
}

func (t *specLiveTarget) send(ctx context.Context, body []byte) (*api.ApplySpecCommandsResponse, error) {
	return t.state.Client.API().ApplySpecCommandsWithBodyWithResponse(
		ctx, t.name, nil, "application/json", bytes.NewReader(body))
}

// applyLocally edits the pulled Live document and sends nothing.
func (t *specLiveTarget) applyLocally(ctx context.Context, commands []apispec.Command) (specEditOutcome, error) {
	data, err := t.Document(ctx)
	if err != nil {
		return specEditOutcome{}, err
	}
	edited, err := apispec.Edit(data, commands...)
	if err != nil {
		return specEditOutcome{}, err
	}
	return specEditOutcome{document: edited}, nil
}

// apiErrorMessage is the message of the first error in an API error answer.
func apiErrorMessage(resp *http.Response, body []byte) string {
	var answer api.ApiErrorResponse
	if err := json.Unmarshal(body, &answer); err == nil && len(answer.Errors) > 0 {
		return answer.Errors[0].Message
	}
	if resp == nil {
		return "the request failed"
	}
	return fmt.Sprintf("api error (%d)", resp.StatusCode)
}

// specPublishedResult is the structured output of an edit of a spec in
// EchoPoint: the Live version, and whether it was made by an earlier request
// with the same command ID.
type specPublishedResult struct {
	api.SpecVersion

	Replayed bool `json:"replayed"`
}

func printSpecPublished(w io.Writer, format output.Format, name string, edit specEdit, published *specPublished) error {
	version := published.version
	result := specPublishedResult{SpecVersion: *version, Replayed: published.replayed}
	if done, err := printStructured(w, format, result); done {
		return err
	}
	if published.replayed {
		_, err := fmt.Fprintf(w, "✓ Already applied: %s %s\n", name, version.Version)
		return err
	}
	fmt.Fprintf(w, "✓ Published %s %s (%s): %s\n", name, version.Version, version.Bump, edit.what())
	printNewFindings(w, version.Findings)
	return nil
}

// printNewFindings lists the convention findings a version introduced, and
// nothing when it introduced none.
func printNewFindings(w io.Writer, findings []api.SpecFinding) {
	var introduced []api.SpecFinding
	for _, finding := range findings {
		if finding.Introduced {
			introduced = append(introduced, finding)
		}
	}
	if len(introduced) == 0 {
		return
	}
	fmt.Fprintf(w, "\nNew findings (%d)\n", len(introduced))
	for _, finding := range introduced {
		fmt.Fprintf(w, "  - %s %s: %s\n", finding.Rule, finding.Pointer, finding.Message)
	}
}
