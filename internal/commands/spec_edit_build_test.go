package commands

import (
	"reflect"
	"strings"
	"testing"

	"github.com/nanostack-dev/echopoint-kit/apispec"
)

func assertCommands(t *testing.T, got []apispec.Command, err error, want ...apispec.Command) {
	t.Helper()
	if err != nil {
		t.Fatalf("builder failed: %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("commands = %+v\nwant      %+v", got, want)
	}
}

func assertBuildError(t *testing.T, err error, contains ...string) {
	t.Helper()
	if err == nil {
		t.Fatalf("builder succeeded, want an error naming %v", contains)
	}
	for _, text := range contains {
		if !strings.Contains(err.Error(), text) {
			t.Errorf("error %q does not mention %q", err, text)
		}
	}
}

func TestBuildRouteAdd(t *testing.T) {
	got, err := buildRouteAdd(routeAddOptions{
		Method: "post", Path: "/pets", OperationID: "createPet", Summary: "Create a pet",
		Description: "Adds a pet.", Tags: []string{"pets"}, Status: "201",
	})
	assertCommands(t, got, err, apispec.Command{
		Kind: apispec.CommandAddOperation, Method: "post", Path: "/pets", OperationID: "createPet",
		Summary: "Create a pet", Description: new("Adds a pet."), Tags: []string{"pets"}, Status: "201",
	})
}

func TestBuildRouteAddNeedsAKnownMethodAndAnAbsolutePath(t *testing.T) {
	_, err := buildRouteAdd(routeAddOptions{Method: "FETCH", Path: "/pets"})
	assertBuildError(t, err, "FETCH", "GET")
	_, err = buildRouteAdd(routeAddOptions{Method: "GET", Path: "pets"})
	assertBuildError(t, err, "path", "/")
}

func TestBuildRouteUpdateOrdersRenamesLast(t *testing.T) {
	got, err := buildRouteUpdate(routeUpdateOptions{
		Method: "get", Path: "/pets",
		NewPath: new("/animals"), NewMethod: new("PUT"),
		OperationID: new("listAnimals"), Summary: new("List"), Description: new(""),
		Tags: []string{"a", "b"}, TagsSet: true,
	})
	pointer := apispec.OperationPointer("get", "/pets")
	assertCommands(t, got, err,
		apispec.Command{Kind: apispec.CommandSetText, Pointer: pointer, Field: "operation_id", Value: "listAnimals"},
		apispec.Command{Kind: apispec.CommandSetText, Pointer: pointer, Field: "summary", Value: "List"},
		apispec.Command{Kind: apispec.CommandSetText, Pointer: pointer, Field: "description"},
		apispec.Command{Kind: apispec.CommandSetTags, Pointer: pointer, Tags: []string{"a", "b"}},
		apispec.Command{Kind: apispec.CommandRenamePath, Pointer: pointer, Path: "/animals"},
		apispec.Command{
			Kind: apispec.CommandSetMethod, Pointer: apispec.OperationPointer("get", "/animals"), Method: "put",
		},
	)
}

func TestBuildRouteUpdateClearTags(t *testing.T) {
	got, err := buildRouteUpdate(routeUpdateOptions{Method: "get", Path: "/pets", ClearTags: true})
	assertCommands(t, got, err,
		apispec.Command{Kind: apispec.CommandSetTags, Pointer: apispec.OperationPointer("get", "/pets")})
}

func TestBuildRouteUpdateRefusals(t *testing.T) {
	_, err := buildRouteUpdate(routeUpdateOptions{Method: "get", Path: "/pets"})
	assertBuildError(t, err, "nothing to change", "--path", "--method", "--summary", "--clear-tags")
	_, err = buildRouteUpdate(routeUpdateOptions{
		Method: "get", Path: "/pets", Tags: []string{"a"}, TagsSet: true, ClearTags: true,
	})
	assertBuildError(t, err, "--tag", "--clear-tags")
}

func TestBuildRouteRemove(t *testing.T) {
	got, err := buildRouteRemove("DELETE", "/pets/{id}")
	assertCommands(
		t,
		got,
		err,
		apispec.Command{
			Kind:    apispec.CommandRemoveOperation,
			Pointer: apispec.OperationPointer("delete", "/pets/{id}"),
		},
	)
}

func TestBuildMethodUpdate(t *testing.T) {
	got, err := buildMethodUpdate("get", "/pets", "Patch")
	assertCommands(
		t,
		got,
		err,
		apispec.Command{
			Kind:    apispec.CommandSetMethod,
			Pointer: apispec.OperationPointer("get", "/pets"),
			Method:  "patch",
		},
	)
}

func TestBuildSchemaAdd(t *testing.T) {
	got, err := buildSchemaAdd(schemaAddOptions{Name: "Pet", Type: "object", Description: "A pet."})
	assertCommands(t, got, err,
		apispec.Command{Kind: apispec.CommandAddSchema, Name: "Pet", Type: "object", Description: new("A pet.")})
	got, err = buildSchemaAdd(schemaAddOptions{Name: "Pet"})
	assertCommands(t, got, err, apispec.Command{Kind: apispec.CommandAddSchema, Name: "Pet"})
}

func TestBuildSchemaUpdate(t *testing.T) {
	pointer := apispec.SchemaPointer("Pet")
	got, err := buildSchemaUpdate(schemaUpdateOptions{Name: "Pet", Description: new("Doc"), Type: new("array")})
	assertCommands(t, got, err,
		apispec.Command{Kind: apispec.CommandSetText, Pointer: pointer, Field: "description", Value: "Doc"},
		apispec.Command{Kind: apispec.CommandSetType, Pointer: pointer, Type: "array"},
	)
	_, err = buildSchemaUpdate(schemaUpdateOptions{Name: "Pet"})
	assertBuildError(t, err, "nothing to change", "--description", "--type")
}

func TestBuildSchemaRemove(t *testing.T) {
	got := buildSchemaRemove("Pet")
	assertCommands(t, got, nil, apispec.Command{Kind: apispec.CommandRemoveSchema, Name: "Pet"})
}

func TestBuildPropertyAdd(t *testing.T) {
	got, err := buildPropertyAdd(propertyAddOptions{
		Schema: "Pet", Name: "status", Type: "string", Format: "x", Nullable: true, Required: new(true),
		Description: "State.", Enum: []string{"a", "b"},
	})
	assertCommands(t, got, err,
		apispec.Command{
			Kind: apispec.CommandAddProperty, Pointer: apispec.SchemaPointer("Pet"), Name: "status", Type: "string",
			Format: "x", Nullable: true, Required: new(true), Description: new("State."),
		},
		apispec.Command{
			Kind: apispec.CommandSetEnum, Pointer: apispec.PropertyPointer("Pet", "status"), Values: []string{"a", "b"},
		},
	)
}

func TestBuildPropertyAddAddressesANestedPropertyWithDots(t *testing.T) {
	got, err := buildPropertyAdd(propertyAddOptions{Schema: "Pet", Name: "address.city", Ref: "City"})
	assertCommands(t, got, err,
		apispec.Command{
			Kind: apispec.CommandAddProperty, Pointer: apispec.PropertyPointer("Pet", "address"), Name: "city",
		},
		apispec.Command{
			Kind: apispec.CommandSetRef, Pointer: apispec.PropertyPointer("Pet", "address", "city"), Schema: "City",
		},
	)
}

func TestBuildPropertyAddRefusals(t *testing.T) {
	_, err := buildPropertyAdd(propertyAddOptions{Schema: "Pet", Name: "a", Ref: "B", Type: "string"})
	assertBuildError(t, err, "--ref", "--type")
	_, err = buildPropertyAdd(propertyAddOptions{Schema: "Pet", Name: "a", Ref: "B", Enum: []string{"x"}})
	assertBuildError(t, err, "--ref", "--enum")
	_, err = buildPropertyAdd(propertyAddOptions{Schema: "Pet", Name: "a..b"})
	assertBuildError(t, err, "a..b")
}

func TestBuildPropertyUpdateOrdersRenameLast(t *testing.T) {
	got, err := buildPropertyUpdate(propertyUpdateOptions{
		Schema: "Pet", Name: "address.city", NewName: new("town"), Type: new("string"), Format: new("f"),
		Nullable: new(true), Required: new(false), Description: new("D"), Enum: []string{"x"}, EnumSet: true,
	})
	pointer := apispec.PropertyPointer("Pet", "address", "city")
	assertCommands(t, got, err,
		apispec.Command{Kind: apispec.CommandSetType, Pointer: pointer, Type: "string", Format: "f", Nullable: true},
		apispec.Command{Kind: apispec.CommandSetEnum, Pointer: pointer, Values: []string{"x"}},
		apispec.Command{Kind: apispec.CommandSetText, Pointer: pointer, Field: "description", Value: "D"},
		apispec.Command{Kind: apispec.CommandSetRequired, Pointer: pointer, Required: new(false)},
		apispec.Command{Kind: apispec.CommandRenameProperty, Pointer: pointer, Name: "town"},
	)
}

func TestBuildPropertyUpdateRefAndClearEnum(t *testing.T) {
	pointer := apispec.PropertyPointer("Pet", "owner")
	got, err := buildPropertyUpdate(propertyUpdateOptions{Schema: "Pet", Name: "owner", Ref: new("Owner")})
	assertCommands(t, got, err, apispec.Command{Kind: apispec.CommandSetRef, Pointer: pointer, Schema: "Owner"})
	got, err = buildPropertyUpdate(propertyUpdateOptions{Schema: "Pet", Name: "owner", ClearEnum: true})
	assertCommands(t, got, err, apispec.Command{Kind: apispec.CommandSetEnum, Pointer: pointer})
}

func TestBuildPropertyUpdateRefusals(t *testing.T) {
	base := propertyUpdateOptions{Schema: "Pet", Name: "a"}
	_, err := buildPropertyUpdate(base)
	assertBuildError(t, err, "nothing to change", "--name", "--clear-enum", "--ref")

	o := base
	o.Format = new("date")
	_, err = buildPropertyUpdate(o)
	assertBuildError(t, err, "--format", "--type")

	o = base
	o.Nullable = new(true)
	_, err = buildPropertyUpdate(o)
	assertBuildError(t, err, "--nullable", "--type")

	o = base
	o.Ref, o.Type = new("B"), new("string")
	_, err = buildPropertyUpdate(o)
	assertBuildError(t, err, "--ref", "--type")

	o = base
	o.Enum, o.EnumSet, o.ClearEnum = []string{"x"}, true, true
	_, err = buildPropertyUpdate(o)
	assertBuildError(t, err, "--enum", "--clear-enum")

	o = base
	o.NewName = new("a.b")
	_, err = buildPropertyUpdate(o)
	assertBuildError(t, err, "--name", "dot")
}

func TestBuildPropertyRemove(t *testing.T) {
	got, err := buildPropertyRemove("Pet", "address.city")
	assertCommands(t, got, err, apispec.Command{
		Kind: apispec.CommandRemoveProperty, Pointer: apispec.PropertyPointer("Pet", "address", "city"),
	})
}

const paramSpec = `openapi: 3.0.3
info: {title: Pets, version: 1.0.0}
paths:
  /pets/{id}:
    parameters:
      - {name: id, in: path, required: true, schema: {type: string}}
    get:
      parameters:
        - {name: limit, in: query, schema: {type: integer}}
        - {name: limit, in: header, schema: {type: integer}}
      responses:
        "200": {description: OK}
`

func TestBuildParamAdd(t *testing.T) {
	got, err := buildParamAdd(paramAddOptions{
		Method: "GET", Path: "/pets", Name: "q", In: "query", Type: "string", Format: "x", Required: new(true),
		Description: "Search.",
	})
	assertCommands(t, got, err, apispec.Command{
		Kind: apispec.CommandAddParameter, Pointer: apispec.OperationPointer("get", "/pets"), Name: "q", In: "query",
		Type: "string", Format: "x", Required: new(true), Description: new("Search."),
	})
}

func TestBuildParamAddNeedsALocation(t *testing.T) {
	_, err := buildParamAdd(paramAddOptions{Method: "GET", Path: "/pets", Name: "q"})
	assertBuildError(t, err, "--in", "query")
	_, err = buildParamAdd(paramAddOptions{Method: "GET", Path: "/pets", Name: "q", In: "body"})
	assertBuildError(t, err, "body", "query")
}

func TestBuildParamUpdateFindsTheParameterByNameAndLocation(t *testing.T) {
	got, err := buildParamUpdate([]byte(paramSpec), paramUpdateOptions{
		Method: "get", Path: "/pets/{id}", Name: "limit", In: "header",
		Type: new("number"), Format: new("double"), Required: new(true), Description: new("Cap"),
		NewName: new("max"), NewIn: new("query"),
	})
	pointer := "/paths/~1pets~1{id}/get/parameters/1"
	assertCommands(t, got, err,
		apispec.Command{Kind: apispec.CommandSetParameterType, Pointer: pointer, Type: "number", Format: "double"},
		apispec.Command{Kind: apispec.CommandSetParameterRequired, Pointer: pointer, Required: new(true)},
		apispec.Command{Kind: apispec.CommandSetParameterField, Pointer: pointer, Field: "description", Value: "Cap"},
		apispec.Command{Kind: apispec.CommandSetParameterField, Pointer: pointer, Field: "name", Value: "max"},
		apispec.Command{Kind: apispec.CommandSetParameterField, Pointer: pointer, Field: "in", Value: "query"},
	)
}

func TestBuildParamUpdateReadsAPathItemParameter(t *testing.T) {
	got, err := buildParamUpdate([]byte(paramSpec), paramUpdateOptions{
		Method: "get", Path: "/pets/{id}", Name: "id", In: "path", Description: new("The id"),
	})
	assertCommands(t, got, err, apispec.Command{
		Kind: apispec.CommandSetParameterField, Pointer: "/paths/~1pets~1{id}/parameters/0", Field: "description",
		Value: "The id",
	})
}

func TestBuildParamUpdateRefusals(t *testing.T) {
	base := paramUpdateOptions{Method: "get", Path: "/pets/{id}", Name: "limit", In: "query"}
	_, err := buildParamUpdate([]byte(paramSpec), base)
	assertBuildError(t, err, "nothing to change", "--new-in", "--required")

	o := base
	o.Format = new("int64")
	_, err = buildParamUpdate([]byte(paramSpec), o)
	assertBuildError(t, err, "--format", "--type")

	o = base
	o.NewIn, o.Required = new("path"), new(false)
	_, err = buildParamUpdate([]byte(paramSpec), o)
	assertBuildError(t, err, "path", "required")

	o = base
	o.Name, o.Description = "missing", new("x")
	_, err = buildParamUpdate([]byte(paramSpec), o)
	assertBuildError(t, err, "missing")
}

func TestBuildParamRemove(t *testing.T) {
	got, err := buildParamRemove([]byte(paramSpec), "get", "/pets/{id}", "limit", "query")
	assertCommands(t, got, err, apispec.Command{
		Kind: apispec.CommandRemoveParameter, Pointer: "/paths/~1pets~1{id}/get/parameters/0",
	})
	_, err = buildParamRemove([]byte(paramSpec), "get", "/pets/{id}", "limit", "cookie")
	assertBuildError(t, err, "limit")
	_, err = buildParamRemove([]byte(paramSpec), "get", "/pets/{id}", "limit", "")
	assertBuildError(t, err, "--in")
}

func TestBuildResponseAdd(t *testing.T) {
	got, err := buildResponseAdd(responseAddOptions{
		Method: "get", Path: "/pets", Status: "4xx", Description: new("Bad"), Schema: "Error",
	})
	assertCommands(t, got, err, apispec.Command{
		Kind: apispec.CommandAddResponse, Pointer: apispec.OperationPointer("get", "/pets"), Status: "4XX",
		Description: new("Bad"), Schema: "Error",
	})
	_, err = buildResponseAdd(
		responseAddOptions{Method: "get", Path: "/pets", Status: "200", Schema: "E", Type: "string"},
	)
	assertBuildError(t, err, "--schema", "--type")
}

func TestBuildResponseUpdate(t *testing.T) {
	got, err := buildResponseUpdate(responseUpdateOptions{
		Method: "get", Path: "/pets", Status: "200", Description: new("Ok"), Type: "string",
	})
	assertCommands(t, got, err, apispec.Command{
		Kind: apispec.CommandSetResponse, Pointer: apispec.ResponsePointer("get", "/pets", "200"),
		Description: new("Ok"), Type: "string",
	})
	_, err = buildResponseUpdate(responseUpdateOptions{Method: "get", Path: "/pets", Status: "200"})
	assertBuildError(t, err, "nothing to change", "--description", "--schema", "--type")
	_, err = buildResponseUpdate(responseUpdateOptions{
		Method: "get", Path: "/pets", Status: "200", Schema: "E", Type: "string",
	})
	assertBuildError(t, err, "--schema", "--type")
}

func TestBuildResponseRemove(t *testing.T) {
	got, err := buildResponseRemove("get", "/pets", "default")
	assertCommands(t, got, err, apispec.Command{
		Kind: apispec.CommandRemoveResponse, Pointer: apispec.ResponsePointer("get", "/pets", "default"),
	})
}
