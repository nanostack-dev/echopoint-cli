package commands

import (
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"

	"github.com/nanostack-dev/echopoint-kit/apispec"
)

// The builders turn the arguments and flags of one spec edit command into the
// apispec.Command values that make the edit. They are pure: nothing here reads
// a file, so the same commands can be applied to a local file or sent to
// EchoPoint. A builder that must find a node by what it holds (a parameter by
// name and location) takes the document bytes it searches.

var httpMethods = []string{
	http.MethodGet, http.MethodPut, http.MethodPost, http.MethodDelete,
	http.MethodOptions, http.MethodHead, http.MethodPatch, http.MethodTrace,
}

func normalizeMethod(method string) (string, error) {
	upper := strings.ToUpper(method)
	if !slices.Contains(httpMethods, upper) {
		return "", fmt.Errorf("%q is not an HTTP method: use %s", method, strings.Join(httpMethods, ", "))
	}
	return strings.ToLower(upper), nil
}

func checkPath(path string) error {
	if !strings.HasPrefix(path, "/") {
		return fmt.Errorf("path %q must start with /", path)
	}
	return nil
}

// operationTarget validates the METHOD and path of an operation and returns
// the method in lower case.
func operationTarget(method, path string) (string, error) {
	lower, err := normalizeMethod(method)
	if err != nil {
		return "", err
	}
	return lower, checkPath(path)
}

func normalizeStatus(status string) string {
	if strings.EqualFold(status, "default") {
		return "default"
	}
	return strings.ToUpper(status)
}

func nothingToChange(flags ...string) error {
	return fmt.Errorf("nothing to change: pass at least one of %s", strings.Join(flags, ", "))
}

func exclusive(flagA, flagB string) error {
	return fmt.Errorf("%s and %s cannot be used together", flagA, flagB)
}

// optionalText is the description a command carries: nil when none was given.
func optionalText(text string) *string {
	if text == "" {
		return nil
	}
	return &text
}

func setText(pointer, field, value string) apispec.Command {
	return apispec.Command{Kind: apispec.CommandSetText, Pointer: pointer, Field: field, Value: value}
}

type routeAddOptions struct {
	Method, Path, OperationID, Summary, Description string
	Tags                                            []string
	Status                                          string
}

func buildRouteAdd(o routeAddOptions) ([]apispec.Command, error) {
	method, err := operationTarget(o.Method, o.Path)
	if err != nil {
		return nil, err
	}
	return []apispec.Command{{
		Kind: apispec.CommandAddOperation, Method: method, Path: o.Path, OperationID: o.OperationID,
		Summary: o.Summary, Description: optionalText(o.Description), Tags: o.Tags, Status: o.Status,
	}}, nil
}

type routeUpdateOptions struct {
	Method, Path                      string
	NewPath, NewMethod                *string
	OperationID, Summary, Description *string
	Tags                              []string
	TagsSet, ClearTags                bool
}

func buildRouteUpdate(o routeUpdateOptions) ([]apispec.Command, error) {
	method, err := operationTarget(o.Method, o.Path)
	if err != nil {
		return nil, err
	}
	if o.TagsSet && o.ClearTags {
		return nil, exclusive("--tag", "--clear-tags")
	}
	pointer := apispec.OperationPointer(method, o.Path)
	var commands []apispec.Command
	for _, text := range []struct {
		field string
		value *string
	}{{"operation_id", o.OperationID}, {"summary", o.Summary}, {"description", o.Description}} {
		if text.value != nil {
			commands = append(commands, setText(pointer, text.field, *text.value))
		}
	}
	if o.TagsSet || o.ClearTags {
		commands = append(commands, apispec.Command{Kind: apispec.CommandSetTags, Pointer: pointer, Tags: o.Tags})
	}
	// Renames go last: each one moves the operation the earlier pointers name.
	if o.NewPath != nil {
		if err = checkPath(*o.NewPath); err != nil {
			return nil, err
		}
		commands = append(
			commands,
			apispec.Command{Kind: apispec.CommandRenamePath, Pointer: pointer, Path: *o.NewPath},
		)
		pointer = apispec.OperationPointer(method, *o.NewPath)
	}
	if o.NewMethod != nil {
		newMethod, methodErr := normalizeMethod(*o.NewMethod)
		if methodErr != nil {
			return nil, methodErr
		}
		commands = append(
			commands,
			apispec.Command{Kind: apispec.CommandSetMethod, Pointer: pointer, Method: newMethod},
		)
	}
	if len(commands) == 0 {
		return nil, nothingToChange("--path", "--method", "--operation-id", "--summary", "--description", "--tag",
			"--clear-tags")
	}
	return commands, nil
}

func buildRouteRemove(method, path string) ([]apispec.Command, error) {
	lower, err := operationTarget(method, path)
	if err != nil {
		return nil, err
	}
	return []apispec.Command{
		{Kind: apispec.CommandRemoveOperation, Pointer: apispec.OperationPointer(lower, path)},
	}, nil
}

func buildMethodUpdate(method, path, to string) ([]apispec.Command, error) {
	lower, err := operationTarget(method, path)
	if err != nil {
		return nil, err
	}
	newMethod, err := normalizeMethod(to)
	if err != nil {
		return nil, err
	}
	return []apispec.Command{{
		Kind: apispec.CommandSetMethod, Pointer: apispec.OperationPointer(lower, path), Method: newMethod,
	}}, nil
}

type schemaAddOptions struct {
	Name, Type, Description string
}

func buildSchemaAdd(o schemaAddOptions) ([]apispec.Command, error) {
	return []apispec.Command{{
		Kind: apispec.CommandAddSchema, Name: o.Name, Type: o.Type, Description: optionalText(o.Description),
	}}, nil
}

type schemaUpdateOptions struct {
	Name              string
	Description, Type *string
}

func buildSchemaUpdate(o schemaUpdateOptions) ([]apispec.Command, error) {
	pointer := apispec.SchemaPointer(o.Name)
	var commands []apispec.Command
	if o.Description != nil {
		commands = append(commands, setText(pointer, "description", *o.Description))
	}
	if o.Type != nil {
		commands = append(commands, apispec.Command{Kind: apispec.CommandSetType, Pointer: pointer, Type: *o.Type})
	}
	if len(commands) == 0 {
		return nil, nothingToChange("--description", "--type")
	}
	return commands, nil
}

func buildSchemaRemove(name string) []apispec.Command {
	return []apispec.Command{{Kind: apispec.CommandRemoveSchema, Name: name}}
}

// propertySegments splits the dotted address of a property: address.city is
// the city property inside the address property.
func propertySegments(name string) ([]string, error) {
	segments := strings.Split(name, ".")
	if slices.Contains(segments, "") {
		return nil, fmt.Errorf("property %q has an empty name: separate nested properties with single dots", name)
	}
	return segments, nil
}

type propertyAddOptions struct {
	Schema, Name, Type, Format string
	Nullable                   bool
	Required                   *bool
	Description                string
	Enum                       []string
	Ref                        string
}

func buildPropertyAdd(o propertyAddOptions) ([]apispec.Command, error) {
	segments, err := propertySegments(o.Name)
	if err != nil {
		return nil, err
	}
	if o.Ref != "" && (o.Type != "" || o.Format != "" || o.Nullable || len(o.Enum) > 0) {
		return nil, errors.New("--ref cannot be combined with --type, --format, --nullable or --enum")
	}
	last := len(segments) - 1
	parent := apispec.SchemaPointer(o.Schema)
	if last > 0 {
		parent = apispec.PropertyPointer(o.Schema, segments[:last]...)
	}
	pointer := apispec.PropertyPointer(o.Schema, segments...)
	commands := []apispec.Command{{
		Kind: apispec.CommandAddProperty, Pointer: parent, Name: segments[last], Type: o.Type, Format: o.Format,
		Nullable: o.Nullable, Required: o.Required, Description: optionalText(o.Description),
	}}
	if len(o.Enum) > 0 {
		commands = append(commands, apispec.Command{Kind: apispec.CommandSetEnum, Pointer: pointer, Values: o.Enum})
	}
	if o.Ref != "" {
		commands = append(commands, apispec.Command{Kind: apispec.CommandSetRef, Pointer: pointer, Schema: o.Ref})
	}
	return commands, nil
}

type propertyUpdateOptions struct {
	Schema, Name                            string
	NewName, Type, Format, Description, Ref *string
	Nullable, Required                      *bool
	Enum                                    []string
	EnumSet, ClearEnum                      bool
}

func (o propertyUpdateOptions) check() error {
	switch {
	case o.Type == nil && o.Format != nil:
		return errors.New("--format is part of the type: pass --type with it")
	case o.Type == nil && o.Nullable != nil:
		return errors.New("--nullable is part of the type: pass --type with it")
	case o.Ref != nil && (o.Type != nil || o.EnumSet || o.ClearEnum):
		return errors.New("--ref cannot be combined with --type, --format, --nullable, --enum or --clear-enum")
	case o.EnumSet && o.ClearEnum:
		return exclusive("--enum", "--clear-enum")
	case o.NewName != nil && strings.Contains(*o.NewName, "."):
		return errors.New("--name is the new name of the property alone: it cannot hold a dot")
	}
	return nil
}

func buildPropertyUpdate(o propertyUpdateOptions) ([]apispec.Command, error) {
	segments, err := propertySegments(o.Name)
	if err != nil {
		return nil, err
	}
	if err = o.check(); err != nil {
		return nil, err
	}
	pointer := apispec.PropertyPointer(o.Schema, segments...)
	var commands []apispec.Command
	if o.Type != nil {
		command := apispec.Command{Kind: apispec.CommandSetType, Pointer: pointer, Type: *o.Type}
		if o.Format != nil {
			command.Format = *o.Format
		}
		command.Nullable = o.Nullable != nil && *o.Nullable
		commands = append(commands, command)
	}
	if o.EnumSet || o.ClearEnum {
		commands = append(commands, apispec.Command{Kind: apispec.CommandSetEnum, Pointer: pointer, Values: o.Enum})
	}
	if o.Ref != nil {
		commands = append(commands, apispec.Command{Kind: apispec.CommandSetRef, Pointer: pointer, Schema: *o.Ref})
	}
	if o.Description != nil {
		commands = append(commands, setText(pointer, "description", *o.Description))
	}
	if o.Required != nil {
		commands = append(
			commands,
			apispec.Command{Kind: apispec.CommandSetRequired, Pointer: pointer, Required: o.Required},
		)
	}
	// The rename goes last: it changes the name the pointer above ends with.
	if o.NewName != nil {
		commands = append(
			commands,
			apispec.Command{Kind: apispec.CommandRenameProperty, Pointer: pointer, Name: *o.NewName},
		)
	}
	if len(commands) == 0 {
		return nil, nothingToChange("--name", "--type", "--format", "--nullable", "--required", "--description",
			"--enum", "--clear-enum", "--ref")
	}
	return commands, nil
}

func buildPropertyRemove(schema, name string) ([]apispec.Command, error) {
	segments, err := propertySegments(name)
	if err != nil {
		return nil, err
	}
	return []apispec.Command{{
		Kind: apispec.CommandRemoveProperty, Pointer: apispec.PropertyPointer(schema, segments...),
	}}, nil
}

var parameterLocations = []string{"query", "path", "header", "cookie"}

func checkLocation(flag, in string) error {
	if in == "" {
		return fmt.Errorf("%s is required: %s", flag, strings.Join(parameterLocations, ", "))
	}
	if !slices.Contains(parameterLocations, in) {
		return fmt.Errorf("%s %q is not a parameter location: use %s", flag, in, strings.Join(parameterLocations, ", "))
	}
	return nil
}

type paramAddOptions struct {
	Method, Path, Name, In, Type, Format string
	Required                             *bool
	Description                          string
}

func buildParamAdd(o paramAddOptions) ([]apispec.Command, error) {
	method, err := operationTarget(o.Method, o.Path)
	if err != nil {
		return nil, err
	}
	if err = checkLocation("--in", o.In); err != nil {
		return nil, err
	}
	return []apispec.Command{{
		Kind: apispec.CommandAddParameter, Pointer: apispec.OperationPointer(method, o.Path), Name: o.Name, In: o.In,
		Type: o.Type, Format: o.Format, Required: o.Required, Description: optionalText(o.Description),
	}}, nil
}

type paramUpdateOptions struct {
	Method, Path, Name, In                    string
	NewName, NewIn, Type, Format, Description *string
	Required                                  *bool
}

func (o paramUpdateOptions) check() error {
	switch {
	case o.Type == nil && o.Format != nil:
		return errors.New("--format is part of the type: pass --type with it")
	case o.NewIn != nil && *o.NewIn == "path" && o.Required != nil && !*o.Required:
		return errors.New("a path parameter must stay required: --new-in path cannot be combined with --required=false")
	case o.NewIn != nil:
		return checkLocation("--new-in", *o.NewIn)
	}
	return nil
}

func buildParamUpdate(document []byte, o paramUpdateOptions) ([]apispec.Command, error) {
	if _, err := operationTarget(o.Method, o.Path); err != nil {
		return nil, err
	}
	if err := checkLocation("--in", o.In); err != nil {
		return nil, err
	}
	if err := o.check(); err != nil {
		return nil, err
	}
	pointer, err := apispec.ParameterPointer(document, o.Method, o.Path, o.Name, o.In)
	if err != nil {
		return nil, err
	}
	var commands []apispec.Command
	if o.Type != nil {
		command := apispec.Command{Kind: apispec.CommandSetParameterType, Pointer: pointer, Type: *o.Type}
		if o.Format != nil {
			command.Format = *o.Format
		}
		commands = append(commands, command)
	}
	if o.Required != nil {
		commands = append(commands, apispec.Command{
			Kind: apispec.CommandSetParameterRequired, Pointer: pointer, Required: o.Required,
		})
	}
	for _, field := range []struct {
		name  string
		value *string
	}{{"description", o.Description}, {"name", o.NewName}, {"in", o.NewIn}} {
		if field.value != nil {
			commands = append(commands, apispec.Command{
				Kind: apispec.CommandSetParameterField, Pointer: pointer, Field: field.name, Value: *field.value,
			})
		}
	}
	if len(commands) == 0 {
		return nil, nothingToChange("--name", "--new-in", "--type", "--format", "--required", "--description")
	}
	return commands, nil
}

func buildParamRemove(document []byte, method, path, name, in string) ([]apispec.Command, error) {
	if _, err := operationTarget(method, path); err != nil {
		return nil, err
	}
	if err := checkLocation("--in", in); err != nil {
		return nil, err
	}
	pointer, err := apispec.ParameterPointer(document, method, path, name, in)
	if err != nil {
		return nil, err
	}
	return []apispec.Command{{Kind: apispec.CommandRemoveParameter, Pointer: pointer}}, nil
}

type responseAddOptions struct {
	Method, Path, Status string
	Description          *string
	Schema, Type         string
}

func buildResponseAdd(o responseAddOptions) ([]apispec.Command, error) {
	method, err := operationTarget(o.Method, o.Path)
	if err != nil {
		return nil, err
	}
	if o.Schema != "" && o.Type != "" {
		return nil, exclusive("--schema", "--type")
	}
	return []apispec.Command{{
		Kind: apispec.CommandAddResponse, Pointer: apispec.OperationPointer(method, o.Path),
		Status: normalizeStatus(o.Status), Description: o.Description, Schema: o.Schema, Type: o.Type,
	}}, nil
}

type responseUpdateOptions struct {
	Method, Path, Status string
	Description          *string
	Schema, Type         string
}

func buildResponseUpdate(o responseUpdateOptions) ([]apispec.Command, error) {
	method, err := operationTarget(o.Method, o.Path)
	if err != nil {
		return nil, err
	}
	switch {
	case o.Schema != "" && o.Type != "":
		return nil, exclusive("--schema", "--type")
	case o.Description == nil && o.Schema == "" && o.Type == "":
		return nil, nothingToChange("--description", "--schema", "--type")
	}
	return []apispec.Command{{
		Kind: apispec.CommandSetResponse, Pointer: apispec.ResponsePointer(method, o.Path, normalizeStatus(o.Status)),
		Description: o.Description, Schema: o.Schema, Type: o.Type,
	}}, nil
}

func buildResponseRemove(method, path, status string) ([]apispec.Command, error) {
	lower, err := operationTarget(method, path)
	if err != nil {
		return nil, err
	}
	return []apispec.Command{{
		Kind: apispec.CommandRemoveResponse, Pointer: apispec.ResponsePointer(lower, path, normalizeStatus(status)),
	}}, nil
}
