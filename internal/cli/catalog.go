package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"slices"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/runnelhq/runnel/internal/action"
	"github.com/runnelhq/runnel/internal/actions"
	"github.com/runnelhq/runnel/internal/schema"
	"github.com/runnelhq/runnel/internal/server"
	"github.com/runnelhq/runnel/internal/transform"
	"github.com/runnelhq/runnel/internal/transforms"
)

func newCatalogCommand() *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "catalog [type]",
		Short: "List the available actions and transforms, or describe one and its fields",
		Long: "Lists the available actions (workflows) and transforms (pipelines) by category, or\n" +
			"describes one type and its configuration fields. A name can exist as action and as\n" +
			"transform (e.g. core.log); both are shown then. With --json, prints the descriptors in\n" +
			"the form the server returns from /api/v1/catalog.",
		Example: "  runnel catalog\n" +
			"  runnel catalog core.wait\n" +
			"  runnel catalog --json",
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			acts, trs := actions.Builtin(), transforms.Builtin()
			out := cmd.OutOrStdout()
			if len(args) == 0 {
				if asJSON {
					return writeJSON(out, server.NewCatalog(acts, trs))
				}
				return printCatalog(out, entries(acts.Descriptors(), trs.Descriptors()))
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
			switch {
			case len(found) == 0:
				return fmt.Errorf("unknown action or transform type %q (run \"runnel catalog\" for a list)", args[0])
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
	return cmd
}

// entry is an action or a transform as the catalog shows it.
type entry struct {
	kind                              string // action or transform
	typ, category, label, description string
	schema                            *schema.Schema
}

func actionEntry(d action.Descriptor) entry {
	return entry{"action", d.Type, d.Category, d.Label, d.Description, d.Schema}
}

func transformEntry(d transform.Descriptor) entry {
	return entry{"transform", d.Type, d.Category, d.Label, d.Description, d.Schema}
}

func entries(acts []action.Descriptor, trs []transform.Descriptor) []entry {
	var out []entry
	for _, d := range acts {
		out = append(out, actionEntry(d))
	}
	for _, d := range trs {
		out = append(out, transformEntry(d))
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

// printCatalog lists actions and transforms grouped by category, in palette order.
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
	p.printf("%s %s – %s (category %s)\n%s\n", e.kind, e.typ, e.label, e.category, e.description)
	if len(e.schema.Properties) == 0 {
		p.printf("\nNo configuration.\n")
	} else {
		p.printf("\nFields (with:):\n")
		printFields(p, e.schema, "")
	}
	if p.err != nil {
		return p.err
	}
	return tw.Flush()
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
