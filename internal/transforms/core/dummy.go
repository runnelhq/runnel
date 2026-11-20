package core

import (
	"context"
	"errors"

	"github.com/runnelhq/runnel/internal/transform"
)

// core.dummy passes all records on unchanged, e.g. as a collection point of branches.
var dummyDefinition = transform.Definition{
	Type:        "core.dummy",
	Category:    transform.CategoryFlow,
	Label:       "Dummy",
	Description: "Passes all records on unchanged",
	New: func(with map[string]any) (transform.Transform, error) {
		if len(with) > 0 {
			return nil, errors.New("core.dummy has no configuration")
		}
		return dummy{}, nil
	},
}

type dummy struct{}

func (dummy) Process(_ context.Context, in <-chan transform.Batch, out *transform.Output) error {
	for b := range in {
		for _, r := range b {
			if err := out.Emit(r); err != nil {
				return err
			}
		}
	}
	return nil
}
