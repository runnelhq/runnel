package http

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"

	"github.com/shopspring/decimal"

	"github.com/runnelhq/runnel/internal/action"
	"github.com/runnelhq/runnel/internal/actions/actiontest"
)

func TestRespondConfig(t *testing.T) {
	actiontest.ConfigError(t, responseDefinition, map[string]any{"code": 200}, `unknown field "code"`)
	actiontest.ConfigError(t, responseDefinition, map[string]any{"headers": "x"}, `field "headers"`)
}

// The first answer is sent; later ones and answers without a caller only log.
func TestRespond(t *testing.T) {
	var sent []action.Response
	first := true
	respond := func(r action.Response) bool {
		sent = append(sent, r)
		ok := first
		first = false
		return ok
	}
	a := actiontest.New(t, responseDefinition, map[string]any{"status": "404", "headers": map[string]any{"X-Reason": "none"},
		"body": map[string]any{"error": "not found"}})
	for i, want := range []struct {
		sent  bool
		level slog.Level
		log   string
	}{
		{true, slog.LevelInfo, "answered the caller with status 404"},
		{false, slog.LevelWarn, "the caller has already been answered; status 404 not sent"},
	} {
		var logs []string
		ec := &action.ExecContext{Respond: respond, Log: func(l slog.Level, msg string) {
			if l == want.level {
				logs = append(logs, msg)
			}
		}}
		res, err := a.Execute(context.Background(), ec)
		out, _ := res.Output.(map[string]any)
		if err != nil || out["sent"] != want.sent || out["status"] != 404 || strings.Join(logs, "") != want.log {
			t.Errorf("%d: %v %v %v", i, out, err, logs)
		}
	}
	if len(sent) != 2 || sent[0].Status != 404 || sent[0].Headers["X-Reason"] != "none" || sent[0].Body.(map[string]any)["error"] != "not found" {
		t.Errorf("sent %+v", sent)
	}

	// Exact numbers stay numbers in the JSON answer.
	sent = nil
	first = true
	a = actiontest.New(t, responseDefinition, map[string]any{"body": map[string]any{"total": decimal.RequireFromString("12.50")}})
	if _, err := a.Execute(context.Background(), &action.ExecContext{Respond: respond, Log: func(slog.Level, string) {}}); err != nil {
		t.Fatal(err)
	}
	if data, err := json.Marshal(sent[0].Body); err != nil || string(data) != `{"total":12.5}` {
		t.Errorf("decimal body: %s %v", data, err)
	}

	// Without a caller: nothing is sent; the default status is 200.
	a = actiontest.New(t, responseDefinition, map[string]any{"body": "ok"})
	res, logs, err := actiontest.Execute(context.Background(), a)
	if out := res.Output.(map[string]any); err != nil || out["sent"] != false || out["status"] != 200 || len(logs) != 1 || logs[0].Level != slog.LevelDebug {
		t.Errorf("without caller: %v %v %v", out, logs, err)
	}
	for _, status := range []string{"abc", "99", "600", "103"} {
		a = actiontest.New(t, responseDefinition, map[string]any{"status": status})
		if _, _, err := actiontest.Execute(context.Background(), a); err == nil || !strings.Contains(err.Error(), "invalid status") {
			t.Errorf("status %s: %v", status, err)
		}
	}
}
