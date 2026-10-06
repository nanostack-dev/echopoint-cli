package output

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"text/tabwriter"

	"gopkg.in/yaml.v3"
)

type Format string

const (
	FormatTable Format = "table"
	FormatJSON  Format = "json"
	FormatYAML  Format = "yaml"
)

func ParseFormat(value string) Format {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case string(FormatJSON):
		return FormatJSON
	case string(FormatYAML):
		return FormatYAML
	default:
		return FormatTable
	}
}

func PrintTable(headers []string, rows [][]string) error {
	return PrintTableTo(os.Stdout, headers, rows)
}

func PrintTableTo(w io.Writer, headers []string, rows [][]string) error {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	if len(headers) > 0 {
		fmt.Fprintln(tw, strings.Join(headers, "\t"))
	}
	for _, row := range rows {
		fmt.Fprintln(tw, strings.Join(row, "\t"))
	}
	return tw.Flush()
}

func PrintJSON(w io.Writer, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(w, string(data))
	return err
}

// PrintYAML writes a value as YAML with the key names of its JSON form: the
// generated API types have only JSON tags, and a YAML encoder would lowercase
// their Go field names. It is the one way the CLI prints YAML.
func PrintYAML(w io.Writer, value any) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	var node yaml.Node
	if err = yaml.Unmarshal(data, &node); err != nil {
		return err
	}
	clearYAMLStyle(&node)
	out, err := yaml.Marshal(&node)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(w, string(out))
	return err
}

func clearYAMLStyle(node *yaml.Node) {
	node.Style = 0
	for _, child := range node.Content {
		clearYAMLStyle(child)
	}
}
