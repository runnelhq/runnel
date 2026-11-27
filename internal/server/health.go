package server

import (
	"crypto/tls"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/runnelhq/runnel/internal/engine/workflow"
	"github.com/runnelhq/runnel/internal/version"
)

// States of GET /health and its checks (S43a).
const (
	HealthOK       = "ok"
	HealthDegraded = "degraded"
	HealthFail     = "fail"
)

// Thresholds of GET /health.
const (
	// CertWarnBefore: the certificate of the listener of http triggers ends sooner.
	CertWarnBefore = 14 * 24 * time.Hour
	// DiskLowBytes and DiskLowPercent: the disk of the data folder is low when less is
	// free than the smaller of both (1 GiB, on small disks 5 %).
	DiskLowBytes   = 1 << 30
	DiskLowPercent = 5
)

// Health is the body of GET /health: the overall status, the checks with their reason
// and the details they are made from. Built from memory and a few file system calls.
type Health struct {
	Status        string        `json:"status"`
	Version       string        `json:"version"`
	UptimeSeconds int64         `json:"uptimeSeconds"`
	StartedAt     time.Time     `json:"startedAt"`
	Project       HealthProject `json:"project"`
	Checks        []HealthCheck `json:"checks"`
	Scheduler     struct {
		Running bool `json:"running"`
		// Triggers is the number of triggers loaded, Active of those enabled.
		Triggers int `json:"triggers"`
		Active   int `json:"active"`
		// Problems are the errors of trigger files; their triggers are not loaded.
		Problems []Problem `json:"problems"`
	} `json:"scheduler"`
	Runs struct {
		Running    int        `json:"running"`
		LastFailed *FailedRun `json:"lastFailed,omitempty"`
		// OnErrorQueue is the number of failed runs waiting for their error workflow.
		OnErrorQueue int `json:"onErrorQueue"`
	} `json:"runs"`
	History struct {
		Enabled  bool   `json:"enabled"`
		Path     string `json:"path,omitempty"`
		Writable bool   `json:"writable"`
		// SizeBytes is the size of the file with its write-ahead log.
		SizeBytes int64  `json:"sizeBytes"`
		Error     string `json:"error,omitempty"`
	} `json:"history"`
	Disk struct {
		Path       string `json:"path"`
		FreeBytes  uint64 `json:"freeBytes"`
		TotalBytes uint64 `json:"totalBytes"`
		Error      string `json:"error,omitempty"`
	} `json:"disk"`
	// Listener is the listener of http triggers; missing if none is configured.
	Listener *HealthListener `json:"listener,omitempty"`
}

// HealthProject names the project of the server.
type HealthProject struct {
	Name string `json:"name"`
	Dir  string `json:"dir"`
}

// HealthCheck is one check of GET /health; Message tells the reason.
type HealthCheck struct {
	Name    string `json:"name"`
	Status  string `json:"status"`
	Message string `json:"message"`
}

// FailedRun is the last run that failed.
type FailedRun struct {
	ID      string    `json:"id"`
	Time    time.Time `json:"time"`
	Path    string    `json:"path"`
	Trigger string    `json:"trigger,omitempty"`
	Error   string    `json:"error,omitempty"`
}

// HealthListener is the state of the listener of http triggers.
type HealthListener struct {
	Configured string `json:"configured"`
	Running    bool   `json:"running"`
	Address    string `json:"address,omitempty"`
	TLS        bool   `json:"tls"`
	// NotAfter is the end of its certificate (HTTPS).
	NotAfter *time.Time `json:"notAfter,omitempty"`
}

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	h := s.health()
	status := http.StatusOK
	if h.Status == HealthFail {
		status = http.StatusServiceUnavailable
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, status, h)
}

// health collects the state of the server; the checks set the overall status.
func (s *Server) health() Health {
	now := s.now()
	h := Health{Status: HealthOK, Version: version.Get().Version, StartedAt: s.started, UptimeSeconds: int64(now.Sub(s.started) / time.Second),
		Project: HealthProject{Name: s.projectName, Dir: s.dir}, Checks: []HealthCheck{}}
	check := func(name, status, msg string) {
		h.Checks = append(h.Checks, HealthCheck{Name: name, Status: status, Message: msg})
		if rank(status) > rank(h.Status) {
			h.Status = status
		}
	}

	// Scheduler and trigger files.
	list := s.triggers.List()
	h.Scheduler.Running, h.Scheduler.Triggers = s.triggers.Running(), len(list)
	h.Scheduler.Problems = s.response(s.triggers.Problems()).Problems
	httpWanted := false
	for _, t := range list {
		if t.Enabled {
			h.Scheduler.Active++
			httpWanted = httpWanted || t.Type == "http"
		}
	}
	if h.Scheduler.Running {
		check("scheduler", HealthOK, fmt.Sprintf("running, %d of %d triggers active", h.Scheduler.Active, len(list)))
	} else {
		check("scheduler", HealthFail, "the scheduler of the triggers is not running")
	}
	if files := problemFiles(h.Scheduler.Problems); len(files) > 0 {
		check("triggers", HealthDegraded, fmt.Sprintf("%d trigger %s with errors, not loaded: %s", len(files), plural(len(files), "file", "files"),
			strings.Join(files, ", ")))
	} else {
		check("triggers", HealthOK, "all trigger files loaded")
	}

	// Runs, and the last run of each active trigger.
	rh := s.runs.Health()
	h.Runs.Running, h.Runs.OnErrorQueue = rh.Running, s.onError.Len()
	if r := rh.LastFailed; r != nil {
		f := &FailedRun{ID: r.ID, Time: r.StartedAt, Path: r.Path, Trigger: r.Trigger, Error: r.Error}
		if r.FinishedAt != nil {
			f.Time = *r.FinishedAt
		}
		h.Runs.LastFailed = f
	}
	var failed []string
	for _, t := range list {
		switch {
		case !t.Enabled:
		case rh.Triggers[t.Type+":"+t.Name] == workflow.Failed:
			failed = append(failed, t.Name+" (failed)")
		case t.Last != nil && t.Last.Error != "" && t.Last.RunID == "":
			failed = append(failed, t.Name+" (not started)")
		}
	}
	if len(failed) > 0 {
		check("runs", HealthDegraded, "last run of active triggers failed: "+strings.Join(failed, ", "))
	} else {
		check("runs", HealthOK, fmt.Sprintf("%d running", rh.Running))
	}

	s.healthHistory(&h, rh.HistoryError, check)
	healthDisk(&h, s.dataDir, s.diskFree, check)
	s.healthListener(&h, httpWanted, now, check)
	return h
}

func (s *Server) healthHistory(h *Health, writeErr string, check func(name, status, msg string)) {
	switch {
	case s.history != nil:
		h.History.Enabled, h.History.Path = true, s.history.Path()
		for _, f := range []string{h.History.Path, h.History.Path + "-wal"} {
			if info, err := os.Stat(f); err == nil {
				h.History.SizeBytes += info.Size()
			}
		}
		if writeErr != "" {
			h.History.Error = writeErr
			check("history", HealthFail, "the run history cannot be written: "+writeErr)
			return
		}
		h.History.Writable = true
		check("history", HealthOK, fmt.Sprintf("writable, %d MB", h.History.SizeBytes>>20))
	case s.historyErr != "":
		h.History.Enabled, h.History.Error = true, s.historyErr
		check("history", HealthFail, "the run history cannot be opened, runs are kept in memory only: "+s.historyErr)
	default:
		check("history", HealthOK, "off, runs are kept in memory only")
	}
}

// healthDisk checks the free space of the disk with dir, the data folder.
func healthDisk(h *Health, dir string, diskFree func(string) (uint64, uint64, error), check func(name, status, msg string)) {
	h.Disk.Path = dir
	free, total, err := diskFree(dir)
	if err != nil {
		h.Disk.Error = err.Error()
		check("disk", HealthOK, "free space unknown: "+err.Error())
		return
	}
	h.Disk.FreeBytes, h.Disk.TotalBytes = free, total
	low := min(uint64(DiskLowBytes), total/100*DiskLowPercent)
	msg := fmt.Sprintf("%d MB free of %d MB in %s", free>>20, total>>20, dir)
	if free < low {
		check("disk", HealthDegraded, "low disk space: "+msg)
	} else {
		check("disk", HealthOK, msg)
	}
}

func (s *Server) healthListener(h *Health, wanted bool, now time.Time, check func(name, status, msg string)) {
	if s.triggerListen == "" {
		return
	}
	l := &HealthListener{Configured: s.triggerListen, Address: s.triggers.HTTPAddress(), TLS: s.triggerTLS != nil}
	l.Running = l.Address != ""
	h.Listener = l
	switch {
	case l.Running:
		check("listener", HealthOK, "http triggers listening on "+l.Address)
	case wanted:
		check("listener", HealthDegraded, "http triggers are enabled but the listener on "+s.triggerListen+" is not open (see the server log)")
	default:
		check("listener", HealthOK, "not open, no http trigger is enabled")
	}
	end, ok := certNotAfter(s.triggerTLS)
	if !ok {
		return
	}
	l.NotAfter = &end
	switch left := end.Sub(now); {
	case left <= 0:
		check("certificate", HealthDegraded, "the certificate of the http triggers has expired on "+end.Format(time.DateOnly))
	case left < CertWarnBefore:
		check("certificate", HealthDegraded, fmt.Sprintf("the certificate of the http triggers expires in %d days (%s)", int(left.Hours()/24), end.Format(time.DateOnly)))
	default:
		check("certificate", HealthOK, "valid until "+end.Format(time.DateOnly))
	}
}

// certNotAfter returns the end of the certificate a TLS configuration serves now.
func certNotAfter(c *tls.Config) (time.Time, bool) {
	if c == nil {
		return time.Time{}, false
	}
	var cert *tls.Certificate
	if len(c.Certificates) > 0 {
		cert = &c.Certificates[0]
	} else if c.GetCertificate != nil {
		cert, _ = c.GetCertificate(&tls.ClientHelloInfo{})
	}
	if cert == nil || cert.Leaf == nil {
		return time.Time{}, false
	}
	return cert.Leaf.NotAfter, true
}

func rank(status string) int {
	switch status {
	case HealthFail:
		return 2
	case HealthDegraded:
		return 1
	}
	return 0
}

// problemFiles returns the files of problems, each once, in their order.
func problemFiles(problems []Problem) []string {
	var files []string
	seen := map[string]bool{}
	for _, p := range problems {
		if !seen[p.File] {
			seen[p.File] = true
			files = append(files, p.File)
		}
	}
	return files
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}

// dataDir returns the folder whose disk GET /health watches: the folder of the run
// histories, else the project folder.
func dataDir(historyBase, project string) string {
	if historyBase == "" {
		return project
	}
	// It may not exist yet (no history opened): look at the nearest folder that does.
	for dir := filepath.Clean(historyBase); ; dir = filepath.Dir(dir) {
		if _, err := os.Stat(dir); err == nil || filepath.Dir(dir) == dir {
			return dir
		}
	}
}
