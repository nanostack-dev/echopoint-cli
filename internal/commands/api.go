package commands

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"

	"echopoint-cli/internal/output"

	"github.com/spf13/cobra"
)

// apiStatusError is a response outside 2xx. The body is already on stdout, so
// the error only names the status; the exit code is 1, as for gh api.
type apiStatusError struct {
	status int
	code   string
}

func (e *apiStatusError) Error() string {
	if e.code != "" {
		return fmt.Sprintf("HTTP %d (%s)", e.status, e.code)
	}
	return fmt.Sprintf("HTTP %d", e.status)
}

func (e *apiStatusError) ExitCode() int { return 1 }

type apiOptions struct {
	method    string
	fields    []string
	rawFields []string
	headers   []string
	file      string
	include   bool
}

func newAPICmd(state *AppState) *cobra.Command {
	opts := &apiOptions{}
	cmd := &cobra.Command{
		Use:   apiCommandName + " <path>",
		Short: "Call any EchoPoint API endpoint with the CLI's credentials",
		Long: `Call any EchoPoint API endpoint with the profile's credentials, organization
and workspace, like 'gh api'. Use it for an endpoint that has no command yet.

The path is relative to the profile's API URL, for example /license/usage. A
full URL is refused, so the credentials never leave the profile's API.

-F/--field adds a field whose value keeps its JSON type: true, false, null and
numbers stay typed, and @<file> reads the value from a file. --raw-field adds a
string field. Fields go to the query string for GET, HEAD and DELETE, and to a
JSON body otherwise. -f/--file sends a file as the body instead (- for stdin).
With a body or fields the method defaults to POST, else to GET.

The response body is printed on stdout, indented when it is JSON, or as YAML
with -o yaml. A status outside 2xx prints the body too and exits 1.

/administrations routes belong to the browser Admin Panel and are refused.`,
		Example: `  echopoint api /license/usage
  echopoint api /me -o yaml
  echopoint api /flows/search -F pagination[limit]=5
  echopoint api /flows/search -f search.json
  echopoint api -X PATCH /organization -F name="Acme Payments"
  echopoint api /license/usage --include`,
		Args: fileFlagArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := requireToken(state); err != nil {
				return err
			}
			req, err := buildAPIRequest(state.Client.BaseURL(), args[0], opts, cmd.Flags().Changed("file"))
			if err != nil {
				return err
			}
			ctx := cmd.Context()
			if ctx == nil {
				ctx = context.Background()
			}
			return sendAPIRequest(ctx, state, req, opts.include, cmd.OutOrStdout())
		},
	}
	cmd.Flags().StringVarP(&opts.method, "method", "X", "",
		"HTTP method (default GET, or POST with fields or a body)")
	cmd.Flags().StringArrayVarP(&opts.fields, "field", "F", nil,
		"Add a typed field key=value (true, false, null, numbers, @file)")
	cmd.Flags().StringArrayVar(&opts.rawFields, "raw-field", nil, "Add a string field key=value")
	cmd.Flags().StringArrayVarP(&opts.headers, "header", "H", nil, "Add a request header key:value")
	cmd.Flags().BoolVarP(&opts.include, "include", "i", false,
		"Print the status line and response headers before the body")
	addFileFlag(cmd, &opts.file, "Request body file, or - for stdin")
	return quietOnError(cmd)
}

// rawAPIRequest is a request relative to the profile's API URL.
type rawAPIRequest struct {
	method       string
	pathAndQuery string
	header       http.Header
	body         []byte
}

func buildAPIRequest(baseURL, path string, opts *apiOptions, hasFile bool) (rawAPIRequest, error) {
	path = strings.TrimSpace(path)
	if strings.Contains(path, "://") {
		return rawAPIRequest{}, fmt.Errorf(
			"%q is a URL; pass a path such as /license/usage, which is sent to %s", path, baseURL,
		)
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	if isAdministrationPath(path) {
		return rawAPIRequest{}, errors.New(
			"/administrations routes belong to the browser Admin Panel; the CLI does not call them",
		)
	}

	fields, err := parseAPIFields(opts.fields, opts.rawFields)
	if err != nil {
		return rawAPIRequest{}, err
	}
	if hasFile && len(fields) > 0 {
		return rawAPIRequest{}, errors.New("pass fields or -f, not both")
	}

	method := strings.ToUpper(strings.TrimSpace(opts.method))
	if method == "" {
		method = http.MethodGet
		if hasFile || len(fields) > 0 {
			method = http.MethodPost
		}
	}

	target, err := url.Parse(path)
	if err != nil {
		return rawAPIRequest{}, fmt.Errorf("invalid path %q: %w", path, err)
	}

	var body []byte
	switch {
	case hasFile:
		body, err = readAPIBody(opts.file)
		if err != nil {
			return rawAPIRequest{}, err
		}
	case len(fields) > 0 && fieldsGoInQuery(method):
		query := target.Query()
		for _, field := range fields {
			query.Add(field.key, queryValue(field.value))
		}
		target.RawQuery = query.Encode()
	case len(fields) > 0:
		object := map[string]any{}
		for _, field := range fields {
			if err := setNestedField(object, field.key, field.value); err != nil {
				return rawAPIRequest{}, err
			}
		}
		body, err = json.Marshal(object)
		if err != nil {
			return rawAPIRequest{}, fmt.Errorf("encode fields: %w", err)
		}
	}

	header := http.Header{}
	header.Set("Accept", "application/json")
	if body != nil {
		header.Set("Content-Type", "application/json")
	}
	for _, entry := range opts.headers {
		name, value, ok := strings.Cut(entry, ":")
		if !ok || strings.TrimSpace(name) == "" {
			return rawAPIRequest{}, fmt.Errorf("header %q is not key:value", entry)
		}
		header.Set(strings.TrimSpace(name), strings.TrimSpace(value))
	}
	return rawAPIRequest{method: method, pathAndQuery: target.String(), header: header, body: body}, nil
}

func isAdministrationPath(path string) bool {
	path, _, _ = strings.Cut(path, "?")
	path = strings.TrimRight(path, "/")
	return path == "/administrations" || strings.HasPrefix(path, "/administrations/")
}

func fieldsGoInQuery(method string) bool {
	return method == http.MethodGet || method == http.MethodHead || method == http.MethodDelete
}

func readAPIBody(file string) ([]byte, error) {
	if file == "-" {
		data, err := io.ReadAll(os.Stdin)
		if err != nil {
			return nil, fmt.Errorf("read stdin: %w", err)
		}
		return data, nil
	}
	data, err := os.ReadFile(file)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", file, err)
	}
	return data, nil
}

type apiField struct {
	key   string
	value any
}

func parseAPIFields(typed, raw []string) ([]apiField, error) {
	fields := make([]apiField, 0, len(typed)+len(raw))
	for _, entry := range typed {
		key, value, err := splitField(entry, "-F")
		if err != nil {
			return nil, err
		}
		parsed, err := typedFieldValue(value)
		if err != nil {
			return nil, fmt.Errorf("field %s: %w", key, err)
		}
		fields = append(fields, apiField{key: key, value: parsed})
	}
	for _, entry := range raw {
		key, value, err := splitField(entry, "--raw-field")
		if err != nil {
			return nil, err
		}
		fields = append(fields, apiField{key: key, value: value})
	}
	return fields, nil
}

func splitField(entry, flag string) (string, string, error) {
	key, value, ok := strings.Cut(entry, "=")
	if !ok || strings.TrimSpace(key) == "" {
		return "", "", fmt.Errorf("%s %q is not key=value", flag, entry)
	}
	return strings.TrimSpace(key), value, nil
}

func typedFieldValue(value string) (any, error) {
	switch value {
	case "true":
		return true, nil
	case "false":
		return false, nil
	case "null":
		return nil, nil
	}
	if file, ok := strings.CutPrefix(value, "@"); ok {
		data, err := readAPIBody(file)
		if err != nil {
			return nil, err
		}
		return string(data), nil
	}
	if number, err := strconv.ParseInt(value, 10, 64); err == nil {
		return number, nil
	}
	if number, err := strconv.ParseFloat(value, 64); err == nil {
		return number, nil
	}
	return value, nil
}

func queryValue(value any) string {
	if value == nil {
		return ""
	}
	return fmt.Sprint(value)
}

// setNestedField writes key into object; a key such as pagination[limit]
// writes {"pagination": {"limit": ...}}.
func setNestedField(object map[string]any, key string, value any) error {
	parts, err := fieldKeyParts(key)
	if err != nil {
		return err
	}
	current := object
	for _, part := range parts[:len(parts)-1] {
		next, exists := current[part]
		if !exists {
			child := map[string]any{}
			current[part] = child
			current = child
			continue
		}
		child, ok := next.(map[string]any)
		if !ok {
			return fmt.Errorf("field %s: %s already holds a value", key, part)
		}
		current = child
	}
	current[parts[len(parts)-1]] = value
	return nil
}

func fieldKeyParts(key string) ([]string, error) {
	name, rest, nested := strings.Cut(key, "[")
	parts := []string{name}
	for nested {
		inner, after, closed := strings.Cut(rest, "]")
		if !closed || inner == "" {
			return nil, fmt.Errorf("field %s: expected name[key]", key)
		}
		parts = append(parts, inner)
		if after == "" {
			break
		}
		if !strings.HasPrefix(after, "[") {
			return nil, fmt.Errorf("field %s: expected name[key]", key)
		}
		rest = after[1:]
	}
	return parts, nil
}

func sendAPIRequest(ctx context.Context, state *AppState, req rawAPIRequest, include bool, w io.Writer) error {
	resp, err := state.Client.Send(ctx, req.method, req.pathAndQuery, req.header, req.body)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read response: %w", err)
	}

	if include {
		fmt.Fprintf(w, "%s %s\n", resp.Proto, resp.Status)
		names := make([]string, 0, len(resp.Header))
		for name := range resp.Header {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			for _, value := range resp.Header[name] {
				fmt.Fprintf(w, "%s: %s\n", name, value)
			}
		}
		fmt.Fprintln(w)
	}
	if err := printAPIBody(w, state.OutputFormat, body); err != nil {
		return err
	}

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return &apiStatusError{status: resp.StatusCode, code: apiErrorCode(body)}
	}
	return nil
}

func printAPIBody(w io.Writer, format output.Format, body []byte) error {
	if len(bytes.TrimSpace(body)) == 0 {
		return nil
	}
	var decoded any
	if err := json.Unmarshal(body, &decoded); err != nil {
		_, writeErr := w.Write(body)
		if writeErr == nil && !bytes.HasSuffix(body, []byte("\n")) {
			_, writeErr = fmt.Fprintln(w)
		}
		return writeErr
	}
	if format == output.FormatYAML {
		return output.PrintYAML(w, decoded)
	}
	var indented bytes.Buffer
	if err := json.Indent(&indented, bytes.TrimSpace(body), "", "  "); err != nil {
		return output.PrintJSON(w, decoded)
	}
	_, err := fmt.Fprintln(w, indented.String())
	return err
}
