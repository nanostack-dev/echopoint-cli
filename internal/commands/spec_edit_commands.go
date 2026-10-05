package commands

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
)

const (
	argsMethodPath       = 2 // METHOD and path
	argsMethodPathName   = 3 // METHOD, path and a name or status
	argsSchemaProperty   = 2 // schema and property
	editInDescription    = "Where the parameter is: query, path, header, or cookie"
	editHelpFileFlag     = "Every edit command takes --file, and edits that file in place. "
	editHelpComments     = "Comments, key order, and untouched lines stay as they were. "
	editHelpDryRun       = "--dry-run prints the edited document instead of writing it. "
	editHelpOutputFormat = "-o json or -o yaml prints {file, commands, changed}, the commands being the edits applied."
)

func editLong(text string) string {
	return text + "\n\n" + editHelpFileFlag + editHelpComments + editHelpDryRun + editHelpOutputFormat
}

func operationLabel(method, path string) string {
	return strings.ToUpper(method) + " " + path
}

func newSpecEditGroup(use, short, long string, children ...*cobra.Command) *cobra.Command {
	cmd := &cobra.Command{Use: use, Short: short, Long: long}
	cmd.AddCommand(children...)
	return cmd
}

func newSpecRouteCmd(state *AppState) *cobra.Command {
	return newSpecEditGroup("route", "Add, update, or remove an operation of a local OpenAPI file",
		"A route is one operation: a METHOD on a path.",
		newSpecRouteAddCmd(state), newSpecRouteUpdateCmd(state), newSpecRouteRemoveCmd(state))
}

func newSpecRouteAddCmd(state *AppState) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "add <METHOD> <path>",
		Short: "Add an operation",
		Long: editLong(`Add an operation to a path, creating the path when it is new. The operation gets
a response for --status (default 200). A {param} in the path that the path item
does not declare is declared as a required string path parameter.`),
		Example: `  echopoint spec route add POST /pets --file openapi.yaml --status 201 --operation-id createPet`,
		Args:    cobra.ExactArgs(argsMethodPath),
	}
	cmd.Flags().String("operation-id", "", "operationId of the operation")
	cmd.Flags().String("summary", "", "Summary of the operation")
	cmd.Flags().String("description", "", "Description of the operation")
	cmd.Flags().StringSlice("tag", nil, "Tag of the operation (repeat or separate with commas)")
	cmd.Flags().String("status", "", "Status of the first response (default 200)")
	return newSpecEditCmd(state, cmd, func(flags flagReader, args []string, _ []byte) (specEdit, error) {
		commands, err := buildRouteAdd(routeAddOptions{
			Method: args[0], Path: args[1], OperationID: flags.str("operation-id"), Summary: flags.str("summary"),
			Description: flags.str("description"), Tags: flags.strs("tag"), Status: flags.str("status"),
		})
		return specEdit{specEditAdd, operationLabel(args[0], args[1]), commands}, err
	})
}

func newSpecRouteUpdateCmd(state *AppState) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "update <METHOD> <path>",
		Short: "Change the text, tags, path, or method of an operation",
		Long: editLong(`Change an operation. An empty --summary, --description, or --operation-id
removes the field. --clear-tags removes the tags.

--path renames the whole path item: every method on the path moves to the new
path, not only <METHOD>. --method changes the method of this operation alone.`),
		Example: `  echopoint spec route update GET /pets --file openapi.yaml --summary "List pets" --tag pets`,
		Args:    cobra.ExactArgs(argsMethodPath),
	}
	cmd.Flags().String("path", "", "New path for the whole path item (every method on it moves)")
	cmd.Flags().String("method", "", "New method for this operation")
	cmd.Flags().String("operation-id", "", "New operationId")
	cmd.Flags().String("summary", "", "New summary")
	cmd.Flags().String("description", "", "New description")
	cmd.Flags().StringSlice("tag", nil, "Replace the tags (repeat or separate with commas)")
	cmd.Flags().Bool("clear-tags", false, "Remove the tags")
	return newSpecEditCmd(state, cmd, func(flags flagReader, args []string, _ []byte) (specEdit, error) {
		commands, err := buildRouteUpdate(routeUpdateOptions{
			Method: args[0], Path: args[1], NewPath: flags.optStr("path"), NewMethod: flags.optStr("method"),
			OperationID: flags.optStr("operation-id"), Summary: flags.optStr("summary"),
			Description: flags.optStr("description"), Tags: flags.strs("tag"), TagsSet: flags.changed("tag"),
			ClearTags: flags.boolean("clear-tags"),
		})
		return specEdit{specEditUpdate, operationLabel(args[0], args[1]), commands}, err
	})
}

func newSpecRouteRemoveCmd(state *AppState) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "remove <METHOD> <path>",
		Short:   "Remove an operation",
		Long:    editLong("Remove an operation. The path goes with it when it has no other operation."),
		Example: `  echopoint spec route remove DELETE /pets/{petId} --file openapi.yaml`,
		Args:    cobra.ExactArgs(argsMethodPath),
	}
	return newSpecEditCmd(state, cmd, func(_ flagReader, args []string, _ []byte) (specEdit, error) {
		commands, err := buildRouteRemove(args[0], args[1])
		return specEdit{specEditRemove, operationLabel(args[0], args[1]), commands}, err
	})
}

func newSpecMethodCmd(state *AppState) *cobra.Command {
	update := &cobra.Command{
		Use:     "update <METHOD> <path> --to <METHOD>",
		Short:   "Change the method of an operation",
		Long:    editLong("Change the method of an operation. It is the same as 'route update --method'."),
		Example: `  echopoint spec method update PUT /pets/{petId} --file openapi.yaml --to PATCH`,
		Args:    cobra.ExactArgs(argsMethodPath),
	}
	update.Flags().String("to", "", "The new method")
	_ = update.MarkFlagRequired("to")
	update = newSpecEditCmd(state, update, func(flags flagReader, args []string, _ []byte) (specEdit, error) {
		commands, err := buildMethodUpdate(args[0], args[1], flags.str("to"))
		return specEdit{specEditUpdate, operationLabel(args[0], args[1]), commands}, err
	})
	return newSpecEditGroup("method", "Change the method of an operation in a local OpenAPI file",
		"A method change keeps the operation and gives it another HTTP method.", update)
}

func newSpecSchemaCmd(state *AppState) *cobra.Command {
	return newSpecEditGroup("schema", "Add, update, or remove a schema of a local OpenAPI file",
		"A schema is an entry of components/schemas.",
		newSpecSchemaAddCmd(state), newSpecSchemaUpdateCmd(state), newSpecSchemaRemoveCmd(state))
}

func newSpecSchemaAddCmd(state *AppState) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "add <Name>",
		Short:   "Add a schema",
		Long:    editLong("Add a schema to components/schemas. Its type is object unless --type says otherwise."),
		Example: `  echopoint spec schema add Pet --file openapi.yaml --description "A pet in the store."`,
		Args:    cobra.ExactArgs(1),
	}
	cmd.Flags().String("type", "", "Type of the schema (default object)")
	cmd.Flags().String("description", "", "Description of the schema")
	return newSpecEditCmd(state, cmd, func(flags flagReader, args []string, _ []byte) (specEdit, error) {
		commands, err := buildSchemaAdd(schemaAddOptions{
			Name: args[0], Type: flags.str("type"), Description: flags.str("description"),
		})
		return specEdit{specEditAdd, "schema " + args[0], commands}, err
	})
}

func newSpecSchemaUpdateCmd(state *AppState) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "update <Name>",
		Short: "Change the description or type of a schema",
		Long: editLong(`Change a schema. An empty --description removes it. --type replaces the type and
resets the format and nullable of the schema.`),
		Example: `  echopoint spec schema update Pet --file openapi.yaml --description "A pet."`,
		Args:    cobra.ExactArgs(1),
	}
	cmd.Flags().String("description", "", "New description")
	cmd.Flags().String("type", "", "New type")
	return newSpecEditCmd(state, cmd, func(flags flagReader, args []string, _ []byte) (specEdit, error) {
		commands, err := buildSchemaUpdate(schemaUpdateOptions{
			Name: args[0], Description: flags.optStr("description"), Type: flags.optStr("type"),
		})
		return specEdit{specEditUpdate, "schema " + args[0], commands}, err
	})
}

func newSpecSchemaRemoveCmd(state *AppState) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "remove <Name>",
		Short: "Remove a schema",
		Long: editLong(`Remove a schema from components/schemas. A schema that is still referenced is
refused, and the refusal names where it is referenced.`),
		Example: `  echopoint spec schema remove Pet --file openapi.yaml`,
		Args:    cobra.ExactArgs(1),
	}
	return newSpecEditCmd(state, cmd, func(_ flagReader, args []string, _ []byte) (specEdit, error) {
		return specEdit{specEditRemove, "schema " + args[0], buildSchemaRemove(args[0])}, nil
	})
}

func newSpecPropertyCmd(state *AppState) *cobra.Command {
	return newSpecEditGroup("property", "Add, update, or remove a property of a schema in a local OpenAPI file",
		`A property is an entry of a schema's properties. Name a nested property with
dots: 'Pet address.city' is the city property of the address property of Pet.`,
		newSpecPropertyAddCmd(state), newSpecPropertyUpdateCmd(state), newSpecPropertyRemoveCmd(state))
}

func newSpecPropertyAddCmd(state *AppState) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "add <Schema> <name>",
		Short: "Add a property",
		Long: editLong(`Add a property to a schema. Its type is string unless --type or --ref says
otherwise. --ref points the property at another schema of components/schemas
and cannot be combined with --type, --format, --nullable, or --enum.`),
		Example: `  echopoint spec property add Pet name --file openapi.yaml --required --description "Name."
  echopoint spec property add Pet address.city --file openapi.yaml
  echopoint spec property add Pet status --file openapi.yaml --enum available,sold`,
		Args: cobra.ExactArgs(argsSchemaProperty),
	}
	cmd.Flags().String("type", "", "Type of the property (default string)")
	cmd.Flags().String("format", "", "Format of the property")
	cmd.Flags().Bool("nullable", false, "The property can be null")
	cmd.Flags().Bool("required", false, "List the property as required")
	cmd.Flags().String("description", "", "Description of the property")
	cmd.Flags().StringSlice("enum", nil, "Allowed values, separated by commas")
	cmd.Flags().String("ref", "", "Schema of components/schemas the property refers to")
	return newSpecEditCmd(state, cmd, func(flags flagReader, args []string, _ []byte) (specEdit, error) {
		commands, err := buildPropertyAdd(propertyAddOptions{
			Schema: args[0], Name: args[1], Type: flags.str("type"), Format: flags.str("format"),
			Nullable: flags.boolean("nullable"), Required: flags.optBool("required"),
			Description: flags.str("description"), Enum: flags.strs("enum"), Ref: flags.str("ref"),
		})
		return specEdit{specEditAdd, fmt.Sprintf("property %s.%s", args[0], args[1]), commands}, err
	})
}

func newSpecPropertyUpdateCmd(state *AppState) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "update <Schema> <name>",
		Short: "Change a property",
		Long: editLong(`Change a property. --format and --nullable are part of the type: pass --type with
them, and --type resets the ones you leave out. An empty --description removes
it. --required=false takes the property off the required list. --enum replaces
the allowed values, --clear-enum removes them. --ref replaces the property with
a reference to another schema and cannot be combined with --type, --format,
--nullable, --enum, or --clear-enum.`),
		Example: `  echopoint spec property update Pet name --file openapi.yaml --name title --required=false
  echopoint spec property update Pet address.city --file openapi.yaml --type string --format city`,
		Args: cobra.ExactArgs(argsSchemaProperty),
	}
	cmd.Flags().String("name", "", "New name of the property (the property alone, no dots)")
	cmd.Flags().String("type", "", "New type")
	cmd.Flags().String("format", "", "New format (with --type)")
	cmd.Flags().Bool("nullable", false, "The property can be null (with --type)")
	cmd.Flags().Bool("required", false, "Whether the property is required: --required or --required=false")
	cmd.Flags().String("description", "", "New description")
	cmd.Flags().StringSlice("enum", nil, "Replace the allowed values, separated by commas")
	cmd.Flags().Bool("clear-enum", false, "Remove the allowed values")
	cmd.Flags().String("ref", "", "Schema of components/schemas the property refers to")
	return newSpecEditCmd(state, cmd, func(flags flagReader, args []string, _ []byte) (specEdit, error) {
		commands, err := buildPropertyUpdate(propertyUpdateOptions{
			Schema: args[0], Name: args[1], NewName: flags.optStr("name"), Type: flags.optStr("type"),
			Format: flags.optStr("format"), Nullable: flags.optBool("nullable"), Required: flags.optBool("required"),
			Description: flags.optStr("description"), Enum: flags.strs("enum"), EnumSet: flags.changed("enum"),
			ClearEnum: flags.boolean("clear-enum"), Ref: flags.optStr("ref"),
		})
		return specEdit{specEditUpdate, fmt.Sprintf("property %s.%s", args[0], args[1]), commands}, err
	})
}

func newSpecPropertyRemoveCmd(state *AppState) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "remove <Schema> <name>",
		Short:   "Remove a property",
		Long:    editLong("Remove a property from a schema, and from its required list."),
		Example: `  echopoint spec property remove Pet address.city --file openapi.yaml`,
		Args:    cobra.ExactArgs(argsSchemaProperty),
	}
	return newSpecEditCmd(state, cmd, func(_ flagReader, args []string, _ []byte) (specEdit, error) {
		commands, err := buildPropertyRemove(args[0], args[1])
		return specEdit{specEditRemove, fmt.Sprintf("property %s.%s", args[0], args[1]), commands}, err
	})
}

func parameterLabel(name, in, method, path string) string {
	return fmt.Sprintf("parameter %s (%s) of %s", name, in, operationLabel(method, path))
}

func newSpecParamCmd(state *AppState) *cobra.Command {
	return newSpecEditGroup("param", "Add, update, or remove a parameter of an operation in a local OpenAPI file",
		`A parameter is found by its name and where it is (--in), never by its position
in the list. A parameter the path item declares for every method is found too.`,
		newSpecParamAddCmd(state), newSpecParamUpdateCmd(state), newSpecParamRemoveCmd(state))
}

func newSpecParamAddCmd(state *AppState) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "add <METHOD> <path> <name> --in <location>",
		Short: "Add a parameter",
		Long: editLong(`Add a parameter to an operation. Its type is string unless --type says otherwise.
A path parameter is always required.`),
		Example: `  echopoint spec param add GET /pets limit --file openapi.yaml --in query --type integer`,
		Args:    cobra.ExactArgs(argsMethodPathName),
	}
	cmd.Flags().String("in", "", editInDescription)
	cmd.Flags().String("type", "", "Type of the parameter (default string)")
	cmd.Flags().String("format", "", "Format of the parameter")
	cmd.Flags().Bool("required", false, "The parameter is required")
	cmd.Flags().String("description", "", "Description of the parameter")
	return newSpecEditCmd(state, cmd, func(flags flagReader, args []string, _ []byte) (specEdit, error) {
		commands, err := buildParamAdd(paramAddOptions{
			Method: args[0], Path: args[1], Name: args[2], In: flags.str("in"), Type: flags.str("type"),
			Format: flags.str("format"), Required: flags.optBool("required"), Description: flags.str("description"),
		})
		return specEdit{specEditAdd, parameterLabel(args[2], flags.str("in"), args[0], args[1]), commands}, err
	})
}

func newSpecParamUpdateCmd(state *AppState) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "update <METHOD> <path> <name> --in <location>",
		Short: "Change a parameter",
		Long: editLong(`Change a parameter. --in names where the parameter is now; --new-in moves it,
--name renames it. --format is part of the type: pass --type with it. A path
parameter stays required. An empty --description removes it.`),
		Example: `  echopoint spec param update GET /pets limit --file openapi.yaml --in query --required=true --type integer`,
		Args:    cobra.ExactArgs(argsMethodPathName),
	}
	cmd.Flags().String("in", "", "Where the parameter is now: query, path, header, or cookie")
	cmd.Flags().String("name", "", "New name of the parameter")
	cmd.Flags().String("new-in", "", "Move the parameter to query, path, header, or cookie")
	cmd.Flags().String("type", "", "New type")
	cmd.Flags().String("format", "", "New format (with --type)")
	cmd.Flags().Bool("required", false, "Whether the parameter is required: --required or --required=false")
	cmd.Flags().String("description", "", "New description")
	return newSpecEditCmd(state, cmd, func(flags flagReader, args []string, document []byte) (specEdit, error) {
		commands, err := buildParamUpdate(document, paramUpdateOptions{
			Method: args[0], Path: args[1], Name: args[2], In: flags.str("in"), NewName: flags.optStr("name"),
			NewIn: flags.optStr("new-in"), Type: flags.optStr("type"), Format: flags.optStr("format"),
			Required: flags.optBool("required"), Description: flags.optStr("description"),
		})
		return specEdit{specEditUpdate, parameterLabel(args[2], flags.str("in"), args[0], args[1]), commands}, err
	})
}

func newSpecParamRemoveCmd(state *AppState) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "remove <METHOD> <path> <name> --in <location>",
		Short:   "Remove a parameter",
		Long:    editLong("Remove a parameter from an operation, or from its path item."),
		Example: `  echopoint spec param remove GET /pets limit --file openapi.yaml --in query`,
		Args:    cobra.ExactArgs(argsMethodPathName),
	}
	cmd.Flags().String("in", "", editInDescription)
	return newSpecEditCmd(state, cmd, func(flags flagReader, args []string, document []byte) (specEdit, error) {
		commands, err := buildParamRemove(document, args[0], args[1], args[2], flags.str("in"))
		return specEdit{specEditRemove, parameterLabel(args[2], flags.str("in"), args[0], args[1]), commands}, err
	})
}

func responseLabel(status, method, path string) string {
	return fmt.Sprintf("response %s of %s", normalizeStatus(status), operationLabel(method, path))
}

func newSpecResponseCmd(state *AppState) *cobra.Command {
	return newSpecEditGroup("response", "Add, update, or remove a response of an operation in a local OpenAPI file",
		`A response is found by its status: a code such as 200, a range such as 4XX, or default.`,
		newSpecResponseAddCmd(state), newSpecResponseUpdateCmd(state), newSpecResponseRemoveCmd(state))
}

func newSpecResponseAddCmd(state *AppState) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "add <METHOD> <path> <status>",
		Short: "Add a response",
		Long: editLong(`Add a response to an operation. Without --description it gets the standard
reason of the status. --schema gives the application/json body a $ref to a
schema of components/schemas; --type gives it an inline type.`),
		Example: `  echopoint spec response add GET /pets/{petId} 404 --file openapi.yaml --schema Error`,
		Args:    cobra.ExactArgs(argsMethodPathName),
	}
	cmd.Flags().String("description", "", "Description of the response")
	cmd.Flags().String("schema", "", "Schema of components/schemas for the JSON body")
	cmd.Flags().String("type", "", "Inline type for the JSON body")
	return newSpecEditCmd(state, cmd, func(flags flagReader, args []string, _ []byte) (specEdit, error) {
		commands, err := buildResponseAdd(responseAddOptions{
			Method: args[0], Path: args[1], Status: args[2], Description: flags.optStr("description"),
			Schema: flags.str("schema"), Type: flags.str("type"),
		})
		return specEdit{specEditAdd, responseLabel(args[2], args[0], args[1]), commands}, err
	})
}

func newSpecResponseUpdateCmd(state *AppState) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "update <METHOD> <path> <status>",
		Short: "Change a response",
		Long: editLong(`Change a response: its description, or its application/json body with --schema
or --type. A response description cannot be empty.`),
		Example: `  echopoint spec response update GET /pets 200 --file openapi.yaml --schema PetList`,
		Args:    cobra.ExactArgs(argsMethodPathName),
	}
	cmd.Flags().String("description", "", "New description")
	cmd.Flags().String("schema", "", "Schema of components/schemas for the JSON body")
	cmd.Flags().String("type", "", "Inline type for the JSON body")
	return newSpecEditCmd(state, cmd, func(flags flagReader, args []string, _ []byte) (specEdit, error) {
		commands, err := buildResponseUpdate(responseUpdateOptions{
			Method: args[0], Path: args[1], Status: args[2], Description: flags.optStr("description"),
			Schema: flags.str("schema"), Type: flags.str("type"),
		})
		return specEdit{specEditUpdate, responseLabel(args[2], args[0], args[1]), commands}, err
	})
}

func newSpecResponseRemoveCmd(state *AppState) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "remove <METHOD> <path> <status>",
		Short:   "Remove a response",
		Long:    editLong("Remove a response from an operation."),
		Example: `  echopoint spec response remove GET /pets 404 --file openapi.yaml`,
		Args:    cobra.ExactArgs(argsMethodPathName),
	}
	return newSpecEditCmd(state, cmd, func(_ flagReader, args []string, _ []byte) (specEdit, error) {
		commands, err := buildResponseRemove(args[0], args[1], args[2])
		return specEdit{specEditRemove, responseLabel(args[2], args[0], args[1]), commands}, err
	})
}
