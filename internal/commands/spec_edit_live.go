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

// specLiveTarget edits the Live version of a spec in EchoPoint: it publishes
// the next Live version with the commands, in one request.
type specLiveTarget struct {
	state     *AppState
	slug      string
	commandID uuid.UUID
	pulled    *api.SpecDocument
}

func (t *specLiveTarget) Name() string { return t.slug }

// Document pulls the Live version, so a parameter is found in the document
// the commands apply to.
func (t *specLiveTarget) Document() ([]byte, error) {
	if t.pulled == nil {
		pulled, err := pullSpec(t.state, t.slug, "")
		if err != nil {
			return nil, err
		}
		t.pulled = pulled
	}
	return []byte(t.pulled.Document), nil
}

func (t *specLiveTarget) Apply(commands []apispec.Command, dryRun bool) (specEditOutcome, error) {
	if dryRun {
		return t.applyLocally(commands)
	}
	body, err := json.Marshal(struct {
		CommandID uuid.UUID         `json:"command_id"`
		Commands  []apispec.Command `json:"commands"`
	}{t.commandID, commands})
	if err != nil {
		return specEditOutcome{}, err
	}
	resp, err := t.send(body)
	if err != nil {
		// A network error does not say whether the commands were applied. The
		// same command ID makes the second try safe: an applied request answers
		// with its version and applies nothing again.
		resp, err = t.send(body)
	}
	if err != nil {
		return specEditOutcome{}, err
	}
	switch {
	case resp.JSON201 != nil:
		return specEditOutcome{published: &specPublished{version: resp.JSON201}, changed: true}, nil
	case resp.JSON200 != nil:
		return specEditOutcome{published: &specPublished{version: resp.JSON200, replayed: true}}, nil
	}
	return specEditOutcome{}, &specRefusedError{message: apiErrorMessage(resp.HTTPResponse, resp.Body)}
}

func (t *specLiveTarget) send(body []byte) (*api.ApplySpecCommandsResponse, error) {
	return t.state.Client.API().ApplySpecCommandsWithBodyWithResponse(
		context.Background(), t.slug, nil, "application/json", bytes.NewReader(body))
}

// applyLocally edits the pulled Live document and sends nothing.
func (t *specLiveTarget) applyLocally(commands []apispec.Command) (specEditOutcome, error) {
	data, err := t.Document()
	if err != nil {
		return specEditOutcome{}, err
	}
	edited, err := apispec.Edit(data, commands...)
	if err != nil {
		return specEditOutcome{}, err
	}
	return specEditOutcome{document: edited, changed: !bytes.Equal(edited, data)}, nil
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

func printSpecPublished(w io.Writer, format output.Format, slug string, edit specEdit, published *specPublished) error {
	version := published.version
	switch format {
	case output.FormatJSON:
		return output.PrintJSON(w, specPublishedResult{SpecVersion: *version, Replayed: published.replayed})
	case output.FormatYAML:
		return printJSONAsYAML(w, specPublishedResult{SpecVersion: *version, Replayed: published.replayed})
	case output.FormatTable:
	}
	if published.replayed {
		_, err := fmt.Fprintf(w, "✓ Already applied: %s %s\n", slug, version.Version)
		return err
	}
	fmt.Fprintf(w, "✓ Published %s %s (%s): %s\n", slug, version.Version, version.Bump, edit.what())
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
