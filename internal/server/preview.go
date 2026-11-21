package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/shopspring/decimal"

	"github.com/runnelhq/runnel/internal/engine/pipeline"
	"github.com/runnelhq/runnel/internal/model"
	"github.com/runnelhq/runnel/internal/record"
	"github.com/runnelhq/runnel/internal/runs"
)

// Limits of the data preview (S44).
var (
	// PreviewTimeout ends a preview that takes longer.
	PreviewTimeout = time.Minute
	// previewSlots limits the previews running at once.
	previewSlots = make(chan struct{}, 2)
)

type previewRequest struct {
	Path    string `json:"path"`
	Content string `json:"content"`
	// Transform is the transform the preview runs up to.
	Transform   string            `json:"transform"`
	Limit       int               `json:"limit,omitempty"`
	Environment string            `json:"environment,omitempty"`
	Params      map[string]string `json:"params,omitempty"`
}

// previewRecords are the records of a link or of the output of the previewed transform.
type previewRecords struct {
	// Rows is the number of records that went over the link (may be more than shown).
	Rows    int64          `json:"rows"`
	Fields  []previewField `json:"fields"`
	Records [][]any        `json:"records"`
}

type previewField struct {
	Name string `json:"name"`
	Type string `json:"type"`
}

type previewResponse struct {
	Status string `json:"status"`
	Error  string `json:"error,omitempty"`
	// FailedAt is the transform the preview stopped at.
	FailedAt string `json:"failedAt,omitempty"`
	Duration int64  `json:"durationMs"`
	Limit    int    `json:"limit"`
	// Counts of the transforms of the preview (the previewed one and those before it), by id.
	Counts map[string]pipeline.Counts `json:"counts"`
	// Links are the records of the links that ran, by index in the list of links.
	Links  map[int]previewRecords `json:"links"`
	Output previewRecords         `json:"output"`
}

// handlePreview runs a pipeline up to a transform with the first records of its inputs
// and returns the records of its links (data preview, S44). Nothing is written and the
// preview is no run: it is not listed and not logged.
func (s *Server) handlePreview(w http.ResponseWriter, r *http.Request) {
	var req previewRequest
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, MaxFileSize+64*1024))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request: "+err.Error())
		return
	}
	name, err := filePath(req.Path)
	if err != nil {
		writeErr(w, err)
		return
	}
	if fileKind(name) != "pipeline" {
		writeError(w, http.StatusBadRequest, "only pipelines (*.pipeline.yaml) have a data preview")
		return
	}
	select {
	case previewSlots <- struct{}{}:
		defer func() { <-previewSlots }()
	default:
		writeError(w, http.StatusTooManyRequests, "other previews are running; try again when they have ended")
		return
	}
	limit := min(max(req.Limit, 1), runs.MaxPreviewLimit)
	if req.Limit == 0 {
		limit = 100
	}
	var mu sync.Mutex
	failedAt := ""
	p, err := runs.PreparePipeline(runs.PipelineSpec{Name: name, Data: []byte(req.Content), ProjectDir: s.dir, Env: req.Environment,
		Params: req.Params, Registry: s.transforms, Preview: &runs.PreviewSpec{UpTo: req.Transform, Limit: limit},
		Observer: func(e pipeline.Event) {
			if e.Kind == pipeline.TransformFinished && e.Status == pipeline.Failed {
				mu.Lock()
				if failedAt == "" {
					failedAt = e.TransformID
				}
				mu.Unlock()
			}
		}})
	var diags model.Errors
	switch {
	case errors.As(err, &diags):
		writeJSON(w, http.StatusUnprocessableEntity, startProblems{
			Error:            "the pipeline has problems; fix the problems listed",
			validateResponse: s.response(diags),
		})
		return
	case err != nil:
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), PreviewTimeout)
	defer cancel()
	res := p.Runner.Run(ctx)

	out := previewResponse{Status: string(res.Status), Duration: res.Duration.Milliseconds(), Limit: limit,
		Counts: map[string]pipeline.Counts{}, Links: map[int]previewRecords{}}
	if res.Err != nil {
		out.Error = res.Err.Error()
		if errors.Is(res.Err, context.DeadlineExceeded) || ctx.Err() != nil {
			out.Error = fmt.Sprintf("the preview took longer than %s and was stopped", PreviewTimeout)
		}
	}
	mu.Lock()
	out.FailedAt = failedAt
	mu.Unlock()
	for _, id := range p.Preview.Transforms {
		out.Counts[id] = res.Counts[id]
	}
	for i, orig := range p.Preview.Links {
		recs := previewOf(res.Samples[i], res.LinkRows[i])
		if i == p.Preview.Output {
			out.Output = recs
		} else if orig >= 0 {
			out.Links[orig] = recs
		}
	}
	writeJSON(w, http.StatusOK, out)
}

// previewOf turns records into rows of values under the fields of all of them, in the
// order they appear; a field's type is that of its first value that is not null.
func previewOf(recs []record.Record, rows int64) previewRecords {
	out := previewRecords{Rows: rows, Fields: []previewField{}, Records: make([][]any, 0, len(recs))}
	index := map[string]int{}
	for _, rec := range recs {
		for _, f := range rec.Fields() {
			i, ok := index[f.Name]
			if !ok {
				i = len(out.Fields)
				index[f.Name] = i
				out.Fields = append(out.Fields, previewField{Name: f.Name})
			}
			if out.Fields[i].Type == "" {
				out.Fields[i].Type = typeName(f.Value)
			}
		}
	}
	for _, rec := range recs {
		row := make([]any, len(out.Fields))
		for _, f := range rec.Fields() {
			row[index[f.Name]] = jsonValue(f.Value)
		}
		out.Records = append(out.Records, row)
	}
	return out
}

// typeName is the type of a value as in "fields:" (ADR 0007); empty for null.
func typeName(v any) string {
	switch v.(type) {
	case nil:
		return ""
	case int64, int:
		return string(record.Int)
	case decimal.Decimal:
		return string(record.Decimal)
	case float64:
		return string(record.Float)
	case bool:
		return string(record.Bool)
	case string:
		return string(record.String)
	case record.Date:
		return string(record.DateType)
	case time.Time:
		return string(record.DateTime)
	}
	return string(record.JSON)
}

// jsonValue writes a value for the editor: numbers as JSON numbers (decimals exactly),
// dates in ISO 8601, nested values as they are.
func jsonValue(v any) any {
	switch v := v.(type) {
	case decimal.Decimal:
		return json.Number(v.String())
	case record.Date:
		return v.String()
	case time.Time:
		return v.Format(time.RFC3339Nano)
	}
	return v
}
