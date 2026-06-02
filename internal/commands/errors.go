package commands

import (
	"encoding/json"
	"fmt"
	"net/http"

	"echopoint-cli/internal/api"
)

func formatAPIError(resp *http.Response, body []byte) error {
	if resp == nil {
		return fmt.Errorf("request failed")
	}

	var apiErr api.ApiErrorResponse
	if err := json.Unmarshal(body, &apiErr); err == nil {
		if len(apiErr.Errors) > 0 {
			return fmt.Errorf("api error (%d): %s", resp.StatusCode, apiErr.Errors[0].Message)
		}
	}

	if len(body) > 0 {
		return fmt.Errorf("api error (%d): %s", resp.StatusCode, string(body))
	}

	return fmt.Errorf("api error (%d)", resp.StatusCode)
}
