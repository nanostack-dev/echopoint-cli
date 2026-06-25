package client

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"echopoint-cli/internal/api"
)

type Client struct {
	api        *api.ClientWithResponses
	httpClient *http.Client
	reqEditor  api.RequestEditorFn
	token      string
	apiKey     string
	baseURL    string
	debug      bool
}

// New creates a new API client with Bearer token auth (for interactive/UI flows).
// organizationID may be empty; when set it is sent as the X-Organization-Id header.
func New(baseURL string, token string, organizationID string, timeout time.Duration) (*Client, error) {
	return newClient(baseURL, token, organizationID, "", timeout)
}

// NewWithAPIKey creates a new API client that uses API key auth instead of Bearer token.
// When apiKey is non-empty it takes precedence: X-Api-Key + X-Organization-Id headers are
// sent and the Authorization header is omitted entirely.
func NewWithAPIKey(baseURL string, apiKey string, organizationID string, timeout time.Duration) (*Client, error) {
	httpClient := &http.Client{Timeout: timeout}
	debug := os.Getenv("ECHOPOINT_DEBUG") != ""

	editor := apiKeyRequestEditor(apiKey, organizationID, debug)
	options := []api.ClientOption{
		api.WithHTTPClient(httpClient),
		api.WithRequestEditorFn(editor),
	}

	apiClient, err := api.NewClientWithResponses(baseURL, options...)
	if err != nil {
		return nil, err
	}

	return &Client{
		api:        apiClient,
		httpClient: httpClient,
		reqEditor:  editor,
		apiKey:     apiKey,
		baseURL:    baseURL,
		debug:      debug,
	}, nil
}

func newClient(
	baseURL string,
	token string,
	organizationID string,
	apiKey string,
	timeout time.Duration,
) (*Client, error) {
	httpClient := &http.Client{Timeout: timeout}
	debug := os.Getenv("ECHOPOINT_DEBUG") != ""

	options := []api.ClientOption{
		api.WithHTTPClient(httpClient),
	}

	var editor api.RequestEditorFn
	if token != "" {
		if organizationID == "" {
			organizationID = strings.TrimSpace(os.Getenv("ECHOPOINT_ORGANIZATION_ID"))
		}
		editor = bearerTokenRequestEditor(token, organizationID, debug)
		options = append(options, api.WithRequestEditorFn(editor))
	}

	apiClient, err := api.NewClientWithResponses(baseURL, options...)
	if err != nil {
		return nil, err
	}

	return &Client{
		api:        apiClient,
		httpClient: httpClient,
		reqEditor:  editor,
		token:      token,
		apiKey:     apiKey,
		baseURL:    baseURL,
		debug:      debug,
	}, nil
}

// bearerTokenRequestEditor returns a request editor that sets Bearer auth.
func bearerTokenRequestEditor(token, organizationID string, debug bool) api.RequestEditorFn {
	return func(ctx context.Context, req *http.Request) error {
		req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", token))
		if organizationID != "" {
			req.Header.Set("X-Organization-Id", organizationID)
		}

		if debug {
			fmt.Fprintf(os.Stderr, "[DEBUG] Request: %s %s\n", req.Method, req.URL)
			fmt.Fprintf(os.Stderr, "[DEBUG] Headers: %s\n", RedactedHeaders(req.Header))
		}

		return nil
	}
}

// apiKeyRequestEditor returns a request editor that sets X-Api-Key + X-Organization-Id
// and omits the Authorization header entirely.
func apiKeyRequestEditor(apiKey, organizationID string, debug bool) api.RequestEditorFn {
	return func(ctx context.Context, req *http.Request) error {
		req.Header.Del("Authorization")
		req.Header.Set("X-Api-Key", apiKey)
		if organizationID != "" {
			req.Header.Set("X-Organization-Id", organizationID)
		}

		if debug {
			fmt.Fprintf(os.Stderr, "[DEBUG] Request: %s %s\n", req.Method, req.URL)
			fmt.Fprintf(os.Stderr, "[DEBUG] Headers: %s\n", RedactedHeaders(req.Header))
		}

		return nil
	}
}

// RedactedHeaders returns a string representation of headers with sensitive values masked.
func RedactedHeaders(h http.Header) string {
	redacted := make(http.Header, len(h))
	for k, v := range h {
		switch strings.ToLower(k) {
		case "authorization", "x-api-key":
			redacted[k] = []string{"[REDACTED]"}
		default:
			redacted[k] = v
		}
	}
	return fmt.Sprintf("%v", map[string][]string(redacted))
}

func (c *Client) BaseURL() string {
	return c.baseURL
}

func (c *Client) Token() string {
	return c.token
}

func (c *Client) APIKey() string {
	return c.apiKey
}

func (c *Client) API() *api.ClientWithResponses {
	return c.api
}

// Do issues a raw HTTP request against the API, applying the same auth headers
// (Bearer/API key + organization) as the generated client. path must already
// have its path parameters substituted; query may be nil; body is raw JSON or
// nil. Returns the status code and response body bytes. Used by the MCP server
// to dispatch any annotated operation generically without per-operation glue.
func (c *Client) Do(
	ctx context.Context,
	method, path string,
	query url.Values,
	body []byte,
) (int, []byte, error) {
	u := strings.TrimRight(c.baseURL, "/") + path
	if len(query) > 0 {
		u += "?" + query.Encode()
	}

	var rdr io.Reader
	if len(body) > 0 {
		rdr = bytes.NewReader(body)
	}

	req, err := http.NewRequestWithContext(ctx, method, u, rdr)
	if err != nil {
		return 0, nil, fmt.Errorf("build request: %w", err)
	}
	if len(body) > 0 {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Accept", "application/json")

	if c.reqEditor != nil {
		if editErr := c.reqEditor(ctx, req); editErr != nil {
			return 0, nil, fmt.Errorf("apply auth: %w", editErr)
		}
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return 0, nil, fmt.Errorf("do request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return resp.StatusCode, nil, fmt.Errorf("read response: %w", err)
	}
	return resp.StatusCode, respBody, nil
}
