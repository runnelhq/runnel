// Package plugintest is a plugin for tests (ADR 0019): a test binary that calls
// ServeIfPlugin in its TestMain becomes a plugin when started with ModeEnv set, and speaks
// the protocol without SDK with the behaviour of the mode:
//
//	normal      describes demo.echo; echo returns its text, logs on stderr
//	parallel    as normal, with concurrency 4
//	v2          answers initialize with protocol version 2
//	other-name  answers initialize as plugin "other"
//	outside     describes core.echo (outside its namespace)
//
// The text "fail" fails, "sleep" takes 300 ms, "crash" ends the process, "hang" waits for
// $/cancelRequest.
package plugintest

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"time"
)

// ModeEnv is the environment variable with the mode.
const ModeEnv = "RUNNEL_TEST_PLUGIN"

// ServeIfPlugin serves as plugin and exits if ModeEnv is set; otherwise it returns.
func ServeIfPlugin() {
	if mode, ok := os.LookupEnv(ModeEnv); ok {
		serve(mode)
		os.Exit(0)
	}
}

func serve(mode string) {
	if os.Getenv("RUNNEL_PLUGIN") != "1" {
		fmt.Fprintln(os.Stderr, "this is a Runnel plugin")
		os.Exit(2)
	}
	var mu sync.Mutex
	out := bufio.NewWriter(os.Stdout)
	send := func(v any) {
		data, _ := json.Marshal(v)
		mu.Lock()
		defer mu.Unlock()
		_, _ = out.Write(append(data, '\n'))
		_ = out.Flush()
	}
	answer := func(id json.RawMessage, result any) {
		send(map[string]any{"jsonrpc": "2.0", "id": id, "result": result})
	}
	fail := func(id json.RawMessage, code int, msg string) {
		send(map[string]any{"jsonrpc": "2.0", "id": id, "error": map[string]any{"code": code, "message": msg}})
	}
	cancelled := make(chan struct{}, 1)
	in := bufio.NewScanner(os.Stdin)
	for in.Scan() {
		var req struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
		}
		_ = json.Unmarshal(in.Bytes(), &req)
		switch req.Method {
		case "initialize":
			version, concurrency, name := 1, 0, "demo"
			switch mode {
			case "v2":
				version = 2
			case "parallel":
				concurrency = 4
			case "other-name":
				name = "other"
			}
			answer(req.ID, map[string]any{"protocolVersion": version, "encoding": "json", "name": name, "version": "1.0.0", "concurrency": concurrency})
		case "describe":
			typ := "demo.echo"
			if mode == "outside" {
				typ = "core.echo"
			}
			answer(req.ID, map[string]any{"actions": []any{map[string]any{
				"type": typ, "category": "general", "label": "Echo", "description": "Returns its text",
				"schema": map[string]any{"type": "object", "required": []string{"text"}, "additionalProperties": false, "properties": map[string]any{
					"text": map[string]any{"type": "string"},
					"api":  map[string]any{"type": "string", "format": "runnel-connection", "x-runnel-connection": []string{"http"}},
				}},
			}}})
		case "action.execute":
			var p struct {
				Config  map[string]any    `json:"config"`
				Context map[string]string `json:"context"`
			}
			_ = json.Unmarshal(req.Params, &p)
			text, _ := p.Config["text"].(string)
			go func(id json.RawMessage) {
				switch text {
				case "fail":
					fail(id, -32000, "cannot echo with password s3cret")
				case "sleep":
					time.Sleep(300 * time.Millisecond)
					answer(id, map[string]any{"output": "slept"})
				case "crash":
					fmt.Fprintln(os.Stderr, "panic: boom")
					os.Exit(3)
				case "hang":
					<-cancelled
					fail(id, -32800, "cancelled")
				default:
					fmt.Fprintln(os.Stderr, "WARN echo "+text+" with s3cret")
					fmt.Fprintln(os.Stderr, "plain line")
					out := map[string]any{"echo": text, "run": p.Context["runId"], "dir": p.Context["projectDir"], "token": "s3cret", "list": []any{"s3cret"}}
					if api, ok := p.Config["api"]; ok {
						out["api"] = api
						out["env"] = p.Context["environment"]
						out["hookToken"] = os.Getenv("HOOK_TOKEN")
					}
					answer(id, map[string]any{"output": out})
				}
			}(req.ID)
		case "$/cancelRequest":
			cancelled <- struct{}{}
		case "shutdown":
			answer(req.ID, nil)
		default:
			if len(req.ID) > 0 {
				fail(req.ID, -32601, "method not found")
			}
		}
	}
}
