package client

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"echopoint-cli/internal/api"
)

type Client struct {
	api     *api.ClientWithResponses
	token   string
	apiKey  string
	baseURL string
	debug   bool
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

	options := []api.ClientOption{
		api.WithHTTPClient(httpClient),
		api.WithRequestEditorFn(apiKeyRequestEditor(apiKey, organizationID, debug)),
	}

	apiClient, err := api.NewClientWithResponses(baseURL, options...)
	if err != nil {
		return nil, err
	}

	return &Client{
		api:     apiClient,
		apiKey:  apiKey,
		baseURL: baseURL,
		debug:   debug,
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

	if token != "" {
		if organizationID == "" {
			organizationID = strings.TrimSpace(os.Getenv("ECHOPOINT_ORGANIZATION_ID"))
		}
		options = append(options, api.WithRequestEditorFn(bearerTokenRequestEditor(token, organizationID, debug)))
	}

	apiClient, err := api.NewClientWithResponses(baseURL, options...)
	if err != nil {
		return nil, err
	}

	return &Client{
		api:     apiClient,
		token:   token,
		apiKey:  apiKey,
		baseURL: baseURL,
		debug:   debug,
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
