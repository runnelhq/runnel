package server

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/goccy/go-yaml"

	"github.com/runnelhq/runnel/internal/model"
)

// formEditRequest carries the text the editor shows; Set changes one field (form of
// S39c2a, E20a), without Set the text is only read.
type formEditRequest struct {
	Path    string           `json:"path"`
	Content string           `json:"content"`
	Set     *model.FileField `json:"set,omitempty"`
}

// connectionForm is what the form of a connection shows: its fields as written.
type connectionForm struct {
	Name        string         `json:"name"`
	Type        string         `json:"type"`
	Description string         `json:"description,omitempty"`
	With        map[string]any `json:"with"`
}

// triggerForm is what the form of a trigger shows (E20a): its fields as written, and
// for cron the schedule in words with the next times.
type triggerForm struct {
	Name        string         `json:"name" yaml:"name"`
	Type        string         `json:"type" yaml:"type"`
	Description string         `json:"description,omitempty" yaml:"description"`
	Target      string         `json:"target" yaml:"target"`
	Environment string         `json:"environment,omitempty" yaml:"environment"`
	Enabled     *bool          `json:"enabled,omitempty" yaml:"enabled"`
	Params      map[string]any `json:"params" yaml:"params"`
	With        map[string]any `json:"with" yaml:"with"`
	Schedule    *scheduleInfo  `json:"schedule,omitempty" yaml:"-"`
}

// scheduleInfo is a cron schedule in words ("" when it cannot be told plainly) and its
// next times in its time zone.
type scheduleInfo struct {
	Text string   `json:"text,omitempty"`
	Next []string `json:"next"`
}

type formEditResponse struct {
	Content    string          `json:"content"`
	Connection *connectionForm `json:"connection,omitempty"`
	Trigger    *triggerForm    `json:"trigger,omitempty"`
	// Removed names the fields under with: a new type of a trigger removed.
	Removed []string `json:"removed,omitempty"`
}

// formKinds are the file kinds the form edits, by the kind of the tree.
var formKinds = map[string]string{"connection": model.KindConnection, "trigger": model.KindTrigger}

// handleEditForm reads the text of a connection or trigger file for the form and applies
// a change of one field (S39c2a, E20a), like /workflows/edit for the canvas; nothing is
// saved.
func (s *Server) handleEditForm(w http.ResponseWriter, r *http.Request) {
	var req formEditRequest
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
	kind, ok := formKinds[fileKind(name)]
	if !ok {
		writeError(w, http.StatusBadRequest, "only connection and trigger files can be edited in the form")
		return
	}
	out := []byte(req.Content)
	var removed []string
	if req.Set != nil {
		if out, removed, err = model.EditFile(kind, name, out, *req.Set); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
	}
	// The form shows a file that reads; without connection or trigger it asks for the
	// YAML view.
	res := formEditResponse{Content: string(out), Removed: removed}
	if kind == model.KindTrigger {
		res.Trigger = readTriggerForm(out, time.Now())
	} else if c, _ := model.ReadConnection(name, out); c != nil {
		with := c.With
		if with == nil {
			with = map[string]any{}
		}
		res.Connection = &connectionForm{Name: c.Name, Type: c.Type, Description: c.Description, With: with}
	}
	writeJSON(w, http.StatusOK, res)
}

// readTriggerForm reads the fields of a trigger as written, also when they do not pass
// the checks yet (e.g. a new type without its required fields); nil when the text is
// not a mapping of the expected shape.
func readTriggerForm(data []byte, now time.Time) *triggerForm {
	var t triggerForm
	if err := yaml.Unmarshal(data, &t); err != nil {
		return nil
	}
	if t.Params == nil {
		t.Params = map[string]any{}
	}
	if t.With == nil {
		t.With = map[string]any{}
	}
	if spec, ok := t.With["schedule"].(string); ok && t.Type == model.TriggerCron {
		tz, _ := t.With["timezone"].(string)
		if times, err := model.ScheduleTimes(spec, tz, now, 3); err == nil {
			info := &scheduleInfo{Text: model.DescribeSchedule(spec), Next: []string{}}
			for _, at := range times {
				info.Next = append(info.Next, at.Format(time.RFC3339))
			}
			t.Schedule = info
		}
	}
	return &t
}
