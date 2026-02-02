package client

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"time"

	"echopoint-cli/internal/api"
)

type Client struct {
	api     *api.ClientWithResponses
	token   string
	baseURL string
	debug   bool
}

func New(baseURL string, token string, timeout time.Duration) (*Client, error) {
	httpClient := &http.Client{Timeout: timeout}

	options := []api.ClientOption{
		api.WithHTTPClient(httpClient),
	}

	// Check if debug mode is enabled
	debug := os.Getenv("ECHOPOINT_DEBUG") != ""

	if token != "" {
		options = append(options, api.WithRequestEditorFn(func(ctx context.Context, req *http.Request) error {
			req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", token))

			// Debug logging
			if debug {
				fmt.Fprintf(os.Stderr, "[DEBUG] Request: %s %s\n", req.Method, req.URL)
				fmt.Fprintf(os.Stderr, "[DEBUG] Headers: %v\n", req.Header)
			}

			return nil
		}))
	}

	apiClient, err := api.NewClientWithResponses(baseURL, options...)
	if err != nil {
		return nil, err
	}

	return &Client{
		api:     apiClient,
		token:   token,
		baseURL: baseURL,
		debug:   debug,
	}, nil
}

func (c *Client) BaseURL() string {
	return c.baseURL
}

func (c *Client) Token() string {
	return c.token
}

func (c *Client) API() *api.ClientWithResponses {
	return c.api
}
