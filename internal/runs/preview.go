package runs

import (
	"fmt"
	"slices"

	"github.com/runnelhq/runnel/internal/model"
	"github.com/runnelhq/runnel/internal/transform"
)

// MaxPreviewLimit is the largest number of records an input reads in a preview.
const MaxPreviewLimit = 1000

// PreviewSpec turns a pipeline run into a data preview (S44): only the transforms up
// to UpTo run, inputs read at most Limit records, outputs write nothing, and the first
// Limit records of each link are kept.
type PreviewSpec struct {
	UpTo  string
	Limit int
}

// previewOutput is the id of the transform that receives the output of the previewed
// transform; it is not a valid id in files, so it cannot collide with one.
const previewOutput = "preview:output"

// Preview tells how the links of a preview pipeline relate to the pipeline.
type Preview struct {
	// Links are the indexes of the links of the pipeline, by index in the preview
	// pipeline; -1 for the link that receives the output of the previewed transform.
	Links []int
	// Output is the index of that link in the preview pipeline.
	Output int
	// Transforms are the ids of the transforms the preview runs: upTo and those before it.
	Transforms []string
}

// previewPipeline returns the part of a pipeline a preview runs: the transforms before
// upTo and upTo itself. Outputs (category output) are replaced by core.dummy, so that
// nothing is written; the transforms after upTo are replaced by core.dummy as well, to
// receive the records of its links, and one more link receives its output.
func previewPipeline(pl *model.Pipeline, upTo string, registry *transform.Registry) (*model.Pipeline, Preview, error) {
	at := slices.IndexFunc(pl.Transforms, func(t model.Transform) bool { return t.ID == upTo })
	if at < 0 {
		return nil, Preview{}, fmt.Errorf("the pipeline has no transform %q", upTo)
	}
	// The transforms before upTo: follow the links backwards.
	keep := map[string]bool{upTo: true}
	for changed := true; changed; {
		changed = false
		for _, l := range pl.Links {
			if keep[l.To] && !keep[l.From] {
				keep[l.From], changed = true, true
			}
		}
	}
	sink := func(id string) model.Transform { return model.Transform{ID: id, Type: "core.dummy"} }
	out := *pl
	out.Transforms, out.Links, out.Layout = nil, nil, nil
	var pv Preview
	for _, t := range pl.Transforms {
		if !keep[t.ID] {
			continue
		}
		pv.Transforms = append(pv.Transforms, t.ID)
		if def, ok := registry.Lookup(t.Type); ok && def.Category == transform.CategoryOutput {
			t = sink(t.ID) // nothing is written
		}
		if t.ID == upTo {
			t.Distribution = "" // every data link gets all records, also the one for the output
		}
		out.Transforms = append(out.Transforms, t)
	}
	sinks := map[string]bool{}
	for i, l := range pl.Links {
		switch {
		case keep[l.From] && keep[l.To]:
		case l.From == upTo:
			if !sinks[l.To] {
				sinks[l.To] = true
				out.Transforms = append(out.Transforms, sink(l.To))
			}
		default:
			continue
		}
		out.Links = append(out.Links, l)
		pv.Links = append(pv.Links, i)
	}
	out.Transforms = append(out.Transforms, sink(previewOutput))
	out.Links = append(out.Links, model.PipelineLink{From: upTo, To: previewOutput})
	pv.Output = len(out.Links) - 1
	pv.Links = append(pv.Links, -1)
	return &out, pv, nil
}
