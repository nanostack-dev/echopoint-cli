package commands

import (
	"encoding/json"
	"os"
)

func loadJSONFile(path string, value any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}

	return json.Unmarshal(data, value)
}
