package cli

import (
	"cmp"
	"encoding/json"
	"fmt"
	"io"
	"slices"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/runnelhq/runnel/internal/action"
	"github.com/runnelhq/runnel/internal/actions"
	"github.com/runnelhq/runnel/internal/connection"
	"github.com/runnelhq/runnel/internal/connections"
	"github.com/runnelhq/runnel/internal/schema"
	"github.com/runnelhq/runnel/internal/server"
	"github.com/runnelhq/runnel/internal/transform"
	"github.com/runnelhq/runnel/internal/transforms"
	"github.com/runnelhq/runnel/internal/version"
)

// catalogReference is the machine-readable reference of the building blocks of a version
// (roadmap E15a): the catalog of /api/v1/catalog plus the version and the built-in
// connection types. Releases attach it as runnel-catalog.json.
type catalogReference struct {
	Version string `json:"version"`
	server.Catalog
	ConnectionTypes []connection.Descriptor `json:"connectionTypes"`
}

// expressionsReference is the reference of expressions of a version: names and functions
// as /api/v1/expressions returns them, without the variables of a project. Releases attach
// it as runnel-expressions.json.
type expressionsReference struct {
	Version string `json:"version"`
	server.Expressions
}

func newCatalogCommand() *cobra.Command {
	var asJSON, expressions bool
	cmd := &cobra.Command{
		Use:   "catalog [type]",
		Short: "List the available actions, transforms and connection types, or describe one and its fields",
		Long: "Lists the available actions (workflows), transforms (pipelines) and connection types,\n" +
			"or describes one type and its configuration fields. A name can exist as action and as\n" +
			"transform (e.g. core.log); both are shown then. With --json, prints the descriptors in\n" +
			"the form the server returns from /api/v1/catalog, with the version and the connection\n" +
			"types. With --expressions, lists the names and functions of expressions instead.",
		Example: "  runnel catalog\n" +
			"  runnel catalog core.wait\n" +
			"  runnel catalog sftp\n" +
			"  runnel catalog --json > runnel-catalog.json\n" +
			"  runnel catalog --expressions --json > runnel-expressions.json",
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			acts, trs, conns := actions.Builtin(), transforms.Builtin(), connections.Builtin()
			out := cmd.OutOrStdout()
			switch {
			case expressions && len(args) > 0:
				return fmt.Errorf("--expressions takes no type")
			case expressions && asJSON:
				return writeJSON(out, expressionsReference{Version: version.Version, Expressions: server.NewExpressions()})
			case expressions:
				return printExpressions(out, server.NewExpressions())
			case len(args) == 0 && asJSON:
				return writeJSON(out, catalogReference{Version: version.Version, Catalog: server.NewCatalog(acts, trs), ConnectionTypes: conns.Descriptors()})
			case len(args) == 0:
				return printCatalog(out, entries(acts.Descriptors(), trs.Descriptors(), conns.Descriptors()))
			}
			var found []entry
			var raw []any
			if d, ok := acts.Descriptor(args[0]); ok {
				found = append(found, actionEntry(d))
				raw = append(raw, d)
			}
			if d, ok := trs.Descriptor(args[0]); ok {
				found = append(found, transformEntry(d))
				raw = append(raw, d)
			}
			if d, ok := conns.Descriptor(args[0]); ok {
				found = append(found, connectionEntry(d))
				raw = append(raw, d)
			}
			switch {
			case len(found) == 0:
				return fmt.Errorf("unknown action, transform or connection type %q (run \"runnel catalog\" for a list)", args[0])
			case asJSON && len(raw) == 1:
				return writeJSON(out, raw[0])
			case asJSON:
				return writeJSON(out, raw) // the same name as action and as transform
			}
			for i, e := range found {
				if i > 0 {
					if _, err := fmt.Fprintln(out); err != nil {
						return err
					}
				}
				if err := printDescriptor(out, e); err != nil {
					return err
				}
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "print descriptors with their JSON Schema")
	cmd.Flags().BoolVar(&expressions, "expressions", false, "list the names and functions of expressions")
	return cmd
}

// entry is an action, a transform or a connection type as the catalog shows it.
type entry struct {
	kind                              string // action, transform or connection
	typ, category, label, description string
	schema                            *schema.Schema
	ref                               action.Reference
}

func actionEntry(d action.Descriptor) entry {
	return entry{"action", d.Type, d.Category, d.Label, d.Description, d.Schema, d.Reference}
}

func transformEntry(d transform.Descriptor) entry {
	return entry{"transform", d.Type, d.Category, d.Label, d.Description, d.Schema, d.Reference}
}

func connectionEntry(d connection.Descriptor) entry {
	return entry{"connection", d.Type, "", d.Label, d.Description, d.Schema, action.Reference{}}
}

func entries(acts []action.Descriptor, trs []transform.Descriptor, conns []connection.Descriptor) []entry {
	var out []entry
	for _, d := range acts {
		out = append(out, actionEntry(d))
	}
	for _, d := range trs {
		out = append(out, transformEntry(d))
	}
	for _, d := range conns {
		out = append(out, connectionEntry(d))
	}
	return out
}

func writeJSON(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

// printer writes formatted text and keeps the first write error.
type printer struct {
	w   io.Writer
	err error
}

func (p *printer) printf(format string, args ...any) {
	if p.err == nil {
		_, p.err = fmt.Fprintf(p.w, format, args...)
	}
}

// printCatalog lists actions and transforms grouped by category, in palette order, then
// the connection types.
func printCatalog(w io.Writer, all []entry) error {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	p := &printer{w: tw}
	for _, kind := range []struct {
		name  string
		order []string
	}{{"action", action.Categories}, {"transform", transform.Categories}} {
		var of []entry
		for _, e := range all {
			if e.kind == kind.name {
				of = append(of, e)
			}
		}
		for _, category := range categoryOrder(of, kind.order) {
			p.printf("%ss / %s\n", kind.name, category)
			for _, e := range of {
				if e.category == category {
					p.printf("  %s\t%s\t%s\n", e.typ, e.label, e.description)
				}
			}
		}
	}
	p.printf("connections\n")
	for _, e := range all {
		if e.kind == "connection" {
			p.printf("  %s\t%s\t%s\n", e.typ, e.label, e.description)
		}
	}
	if p.err != nil {
		return p.err
	}
	return tw.Flush()
}

// printExpressions lists the names and functions of expressions, functions by group.
func printExpressions(w io.Writer, ref server.Expressions) error {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	p := &printer{w: tw}
	p.printf("names\n")
	for _, d := range ref.Roots {
		p.printf("  %s\t%s\t%s\n", d.Name, d.Type, d.Description)
	}
	var groups []string
	for _, d := range ref.Functions {
		if !slices.Contains(groups, d.Group) {
			groups = append(groups, d.Group)
		}
	}
	for _, g := range groups {
		p.printf("functions / %s\n", g)
		for _, d := range ref.Functions {
			if d.Group == g {
				p.printf("  %s\t%s\t%s\n", cmp.Or(d.Signature, d.Name), d.Type, d.Description)
			}
		}
	}
	if p.err != nil {
		return p.err
	}
	return tw.Flush()
}

// categoryOrder returns the categories in use: known ones in palette order, then others.
func categoryOrder(of []entry, order []string) []string {
	var out []string
	for _, c := range order {
		if slices.ContainsFunc(of, func(e entry) bool { return e.category == c }) {
			out = append(out, c)
		}
	}
	for _, e := range of {
		if !slices.Contains(out, e.category) {
			out = append(out, e.category)
		}
	}
	return out
}

// printDescriptor describes one action or transform and its configuration fields.
func printDescriptor(w io.Writer, e entry) error {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	p := &printer{w: tw}
	if e.category != "" {
		p.printf("%s %s – %s (category %s)\n%s\n", e.kind, e.typ, e.label, e.category, e.description)
	} else {
		p.printf("%s %s – %s\n%s\n", e.kind, e.typ, e.label, e.description)
	}
	if len(e.schema.Properties) == 0 {
		p.printf("\nNo configuration.\n")
	} else {
		p.printf("\nFields (with:):\n")
		printFields(p, e.schema, "")
	}
	printReference(p, e)
	if p.err != nil {
		return p.err
	}
	return tw.Flush()
}

// printReference describes the output, the records, the errors and the limits of a type
// (roadmap E15b).
func printReference(p *printer, e entry) {
	if len(e.ref.Outputs) > 0 {
		p.printf("\n%s\n", map[string]string{"action": "Output (result.output):", "transform": "Fields set:"}[e.kind])
		for _, o := range e.ref.Outputs {
			p.printf("  %s\t%s\t%s\n", o.Name, o.Type, o.Description)
		}
	}
	for _, s := range []struct{ title, text string }{{"Records", e.ref.Records}, {"Errors", e.ref.Errors}} {
		if s.text != "" {
			p.printf("\n%s:\n  %s\n", s.title, s.text)
		}
	}
	if len(e.ref.Limits) > 0 {
		p.printf("\nLimits:\n")
		for _, l := range e.ref.Limits {
			p.printf("  - %s\n", l)
		}
	}
}

func printFields(p *printer, s *schema.Schema, prefix string) {
	for _, prop := range s.Properties {
		name := prefix + prop.Name
		p.printf("  %s\t%s\n  \t%s\n", name, prop.Schema.Description, fieldFacts(prop.Schema, slices.Contains(s.Required, prop.Name)))
		switch {
		case len(prop.Schema.Properties) > 0:
			printFields(p, prop.Schema, name+".")
		case prop.Schema.Items != nil && len(prop.Schema.Items.Properties) > 0:
			printFields(p, prop.Schema.Items, name+"[].")
		}
	}
}

// fieldFacts summarizes type and options of a field, e.g. "string, required, e.g. 30s".
func fieldFacts(s *schema.Schema, required bool) string {
	facts := []string{typeName(s)}
	if required {
		facts = append(facts, "required")
	}
	switch s.Format {
	case schema.FormatDuration:
		facts = append(facts, "duration")
	case schema.FormatText:
		facts = append(facts, "multi-line")
	}
	if len(s.Enum) > 0 {
		values := make([]string, len(s.Enum))
		for i, v := range s.Enum {
			values[i] = fmt.Sprint(v)
		}
		facts = append(facts, "one of "+strings.Join(values, "|"))
	}
	if s.Default != nil {
		facts = append(facts, fmt.Sprintf("default %v", s.Default))
	}
	if len(s.Examples) > 0 {
		facts = append(facts, fmt.Sprintf("e.g. %v", s.Examples[0]))
	}
	return strings.Join(facts, ", ")
}

func typeName(s *schema.Schema) string {
	switch {
	case s.Type == "":
		return "any"
	case s.Type == "array" && s.Items != nil:
		return "list of " + typeName(s.Items)
	}
	return s.Type
}
