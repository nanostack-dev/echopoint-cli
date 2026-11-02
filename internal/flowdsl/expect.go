package flowdsl

import (
	"fmt"
	"slices"
	"strconv"
	"strings"

	"echopoint-cli/internal/api"

	"gopkg.in/yaml.v3"
)

// The operator list matches `flow node expect`. between is the one addition:
// the API has it, and the expect command does not.
var expectOperators = []string{
	"equals", "not_equals", "contains", "not_contains", "greater_than", "less_than",
	"greater_than_or_equal", "less_than_or_equal", "empty", "not_empty", "starts_with",
	"ends_with", "regex", "between",
}

var valuelessOperators = []string{"empty", "not_empty"}

func parseExpectNode(n *yaml.Node) ([]api.CompositeAssertion, error) {
	if n == nil {
		return nil, nil
	}
	switch n.Kind {
	case yaml.ScalarNode:
		one, err := parseExpectLine(n.Value)
		if err != nil {
			return nil, fmt.Errorf("line %d: %w", n.Line, err)
		}
		return []api.CompositeAssertion{one}, nil
	case yaml.SequenceNode:
		out := make([]api.CompositeAssertion, 0, len(n.Content))
		for _, item := range n.Content {
			one, err := parseExpectItem(item)
			if err != nil {
				return nil, err
			}
			out = append(out, one)
		}
		return out, nil
	case yaml.MappingNode:
		one, err := parseStatusMapping(n)
		if err != nil {
			return nil, err
		}
		return []api.CompositeAssertion{one}, nil
	default:
		return nil, fmt.Errorf("line %d: expected a check", n.Line)
	}
}

func parseExpectItem(n *yaml.Node) (api.CompositeAssertion, error) {
	if text, ok := scalar(n); ok {
		one, err := parseExpectLine(text)
		if err != nil {
			return api.CompositeAssertion{}, fmt.Errorf("line %d: %w", n.Line, err)
		}
		return one, nil
	}
	if n.Kind == yaml.MappingNode {
		return parseStatusMapping(n)
	}
	return api.CompositeAssertion{}, fmt.Errorf("line %d: expected a check", n.Line)
}

// parseStatusMapping reads `status: 200`. The value is kept as text: that is
// the wire form `flow node expect` and build_flow already send.
func parseStatusMapping(n *yaml.Node) (api.CompositeAssertion, error) {
	m, err := mappingOf(n)
	if err != nil {
		return api.CompositeAssertion{}, err
	}
	status, ok := m.take("status")
	if !ok || len(m.leftover()) > 0 {
		return api.CompositeAssertion{}, fmt.Errorf(
			"line %d: a check mapping only accepts status, or write \"$.path equals value\"", n.Line)
	}
	text, ok := scalar(status)
	if !ok {
		return api.CompositeAssertion{}, fmt.Errorf("line %d: status needs a value", status.Line)
	}
	if _, err := strconv.Atoi(strings.TrimSpace(text)); err != nil {
		return api.CompositeAssertion{}, fmt.Errorf("line %d: status %q is not a number", status.Line, text)
	}
	return statusEquals(text), nil
}

func parseExpectLine(line string) (api.CompositeAssertion, error) {
	// Trim the left side and a trailing newline only. A value may end with a
	// space, and TrimSpace would delete it.
	text := strings.TrimLeft(line, " \t")
	text = strings.TrimRight(text, "\r\n")
	if strings.TrimSpace(text) == "" {
		return api.CompositeAssertion{}, fmt.Errorf("empty check")
	}
	if code, ok := strings.CutPrefix(text, "status:"); ok {
		code = strings.TrimSpace(code)
		if _, err := strconv.Atoi(code); err != nil {
			return api.CompositeAssertion{}, fmt.Errorf("status %q is not a number", code)
		}
		return statusEquals(code), nil
	}
	return parseCheck(text)
}

func statusEquals(code string) api.CompositeAssertion {
	return api.CompositeAssertion{
		ExtractorType: api.ExtractorTypeStatusCode,
		ExtractorData: map[string]any{},
		OperatorType:  api.OperatorTypeEquals,
		OperatorData:  map[string]any{"value": code},
	}
}

func parseCheck(match string) (api.CompositeAssertion, error) {
	target, operator, value, hasValue, err := splitCheck(match)
	if err != nil {
		return api.CompositeAssertion{}, err
	}
	if operator == "" {
		return api.CompositeAssertion{}, fmt.Errorf(
			"%q: expected \"<target> <operator> [value]\", such as \"$.id not_empty\"", match)
	}
	if !slices.Contains(expectOperators, operator) {
		return api.CompositeAssertion{}, fmt.Errorf("%q: unknown operator %q", match, operator)
	}
	assertion := api.CompositeAssertion{
		OperatorType:  api.OperatorType(operator),
		OperatorData:  map[string]any{},
		ExtractorData: map[string]any{},
	}
	switch {
	case strings.HasPrefix(target, "$"):
		assertion.ExtractorType = api.ExtractorTypeJsonPath
		assertion.ExtractorData["path"] = target
	case strings.HasPrefix(target, "header:") && len(target) > len("header:"):
		assertion.ExtractorType = api.ExtractorTypeHeader
		assertion.ExtractorData["header_name"] = strings.TrimPrefix(target, "header:")
	case strings.HasPrefix(target, "query:") && len(target) > len("query:"):
		assertion.ExtractorType = api.ExtractorTypeQueryParam
		assertion.ExtractorData["param_name"] = strings.TrimPrefix(target, "query:")
	case target == string(api.ExtractorTypeBody):
		assertion.ExtractorType = api.ExtractorTypeBody
	case target == statusWord:
		assertion.ExtractorType = api.ExtractorTypeStatusCode
	default:
		return api.CompositeAssertion{}, fmt.Errorf(
			"%q: target must be a JSONPath ($.id), header:<name>, query:<name>, body, or status: <code>", match)
	}
	if operator == string(api.OperatorTypeBetween) {
		return finishBetween(assertion, match, value, hasValue)
	}
	valueless := slices.Contains(valuelessOperators, operator)
	switch {
	case valueless && hasValue:
		return api.CompositeAssertion{}, fmt.Errorf("%q: %s takes no value", match, operator)
	case !valueless && !hasValue:
		return api.CompositeAssertion{}, fmt.Errorf("%q: %s needs a value", match, operator)
	case hasValue:
		assertion.OperatorData["value"] = value
	}
	return assertion, nil
}

// splitCheck reads a target, then an operator, then the rest of the line.
// A target that contains a space is a quoted JSONPath, such as a filter.
// The value is not trimmed: it may end with a space.
func splitCheck(match string) (string, string, string, bool, error) {
	target, text, err := takeTarget(strings.TrimLeft(match, " \t"))
	if err != nil {
		return "", "", "", false, fmt.Errorf("%q: %w", match, err)
	}
	if text == "" {
		return target, "", "", false, nil
	}
	operator, value, hasValue := strings.Cut(text, " ")
	if hasValue && strings.TrimSpace(value) == "" && slices.Contains(valuelessOperators, operator) {
		return target, operator, "", false, nil
	}
	return target, operator, value, hasValue, nil
}

func takeTarget(text string) (string, string, error) {
	if !strings.HasPrefix(text, `"`) {
		head, rest, found := strings.Cut(text, " ")
		if !found {
			return head, "", nil
		}
		return head, rest, nil
	}
	quoted, err := strconv.QuotedPrefix(text)
	if err != nil {
		return "", "", err
	}
	target, err := strconv.Unquote(quoted)
	if err != nil {
		return "", "", err
	}
	return target, strings.TrimLeft(text[len(quoted):], " \t"), nil
}

func finishBetween(
	assertion api.CompositeAssertion,
	match, value string,
	hasValue bool,
) (api.CompositeAssertion, error) {
	if !hasValue {
		return api.CompositeAssertion{}, fmt.Errorf("%q: between needs two values, such as \"$.n between 1 5\"", match)
	}
	fields := strings.Fields(value)
	if len(fields) != 2 {
		return api.CompositeAssertion{}, fmt.Errorf("%q: between needs two values, such as \"$.n between 1 5\"", match)
	}
	// The runner reads operator_data.value as a [min, max] pair, not min/max keys.
	assertion.OperatorData = map[string]any{"value": []any{boundToken(fields[0]), boundToken(fields[1])}}
	return assertion, nil
}

// boundToken keeps a numeric bound a number so the pair matches the stored
// form. A template such as {{min}} stays text.
func boundToken(text string) any {
	if value, err := strconv.Atoi(text); err == nil {
		return value
	}
	if value, err := strconv.ParseFloat(text, 64); err == nil {
		return value
	}
	return text
}
