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
)

func newCatalogCommand() *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "catalog [type]",
		Short: "List the available actions, or describe one action and its fields",
		Long: "Lists the available actions by category, or describes one action and its configuration\n" +
			"fields. With --json, prints the descriptors in the form the server returns from /api/v1/catalog.",
		Example: "  runnel catalog\n" +
			"  runnel catalog core.wait\n" +
			"  runnel catalog --json",
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			r := actions.Builtin()
			out := cmd.OutOrStdout()
			if len(args) == 0 {
				if asJSON {
					return writeJSON(out, server.NewCatalog(r))
				}
				return printCatalog(out, r.Descriptors())
			}
			d, ok := r.Descriptor(args[0])
			if !ok {
				return fmt.Errorf("unknown action type %q (run \"runnel catalog\" for a list)", args[0])
			}
			if asJSON {
				return writeJSON(out, d)
			}
			return printDescriptor(out, d)
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "print descriptors with their JSON Schema")
	return cmd
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

// printCatalog lists the actions grouped by category, in palette order.
func printCatalog(w io.Writer, descs []action.Descriptor) error {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	p := &printer{w: tw}
	for _, category := range categoryOrder(descs) {
		p.printf("%s\n", category)
		for _, d := range descs {
			if d.Category == category {
				p.printf("  %s\t%s\t%s\n", d.Type, d.Label, d.Description)
			}
		}
	}
	if p.err != nil {
		return p.err
	}
	return tw.Flush()
}

// categoryOrder returns the categories in use: known ones in palette order, then others.
func categoryOrder(descs []action.Descriptor) []string {
	var out []string
	for _, c := range action.Categories {
		if slices.ContainsFunc(descs, func(d action.Descriptor) bool { return d.Category == c }) {
			out = append(out, c)
		}
	}
	for _, d := range descs {
		if !slices.Contains(out, d.Category) {
			out = append(out, d.Category)
		}
	}
	return out
}

// printDescriptor describes one action and its configuration fields.
func printDescriptor(w io.Writer, d action.Descriptor) error {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	p := &printer{w: tw}
	p.printf("%s – %s (category %s)\n%s\n", d.Type, d.Label, d.Category, d.Description)
	if len(d.Schema.Properties) == 0 {
		p.printf("\nNo configuration.\n")
	} else {
		p.printf("\nFields (with:):\n")
		printFields(p, d.Schema, "")
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
	if s.Format == schema.FormatDuration {
		facts = append(facts, "duration")
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
