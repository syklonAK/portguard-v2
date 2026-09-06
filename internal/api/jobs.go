// Background job runner for long shell operations (tool installs, ACME
// issuance): each job captures its command output line-by-line, publishes it
// to the SSE bus as a "job" event and keeps a bounded buffer so the UI can
// show a live terminal view and late joiners can fetch the full transcript.
package api

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/http"
	"sort"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
)

const maxJobLines = 500

// Job is one tracked long-running operation.
type Job struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Status    string    `json:"status"` // running | success | failed
	Error     string    `json:"error,omitempty"`
	StartedAt time.Time `json:"started_at"`
	Finished  *time.Time `json:"finished_at,omitempty"`

	mu    sync.Mutex
	lines []string
	sink  func(string) // nil-safe; set by the manager
}

// Write records one output line and streams it to SSE subscribers.
func (j *Job) Write(line string) {
	j.mu.Lock()
	if len(j.lines) >= maxJobLines {
		j.lines = append(j.lines[1:], line)
	} else {
		j.lines = append(j.lines, line)
	}
	j.mu.Unlock()
	if j.sink != nil {
		j.sink(line)
	}
}

// Output returns the full transcript (oldest first).
func (j *Job) Output() []string {
	j.mu.Lock()
	defer j.mu.Unlock()
	out := make([]string, len(j.lines))
	copy(out, j.lines)
	return out
}

// JobManager owns all jobs for this process.
type JobManager struct {
	mu   sync.Mutex
	jobs map[string]*Job
	next func(event string, payload any) // SSE publisher
}

func NewJobManager(publish func(string, any)) *JobManager {
	return &JobManager{jobs: map[string]*Job{}, next: publish}
}

func randomID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// Start runs fn in a background goroutine as a tracked job. The returned
// job is immediately visible with status "running".
func (m *JobManager) Start(name string, fn func(job *Job) error) *Job {
	j := &Job{ID: randomID(), Name: name, Status: "running", StartedAt: time.Now()}
	m.mu.Lock()
	m.jobs[j.ID] = j
	m.mu.Unlock()
	j.sink = func(line string) {
		if m.next != nil {
			m.next("job", map[string]any{"id": j.ID, "name": j.Name, "status": j.Status, "line": line})
		}
	}
	go func() {
		err := fn(j)
		now := time.Now()
		j.mu.Lock()
		j.Finished = &now
		if err != nil {
			j.Status = "failed"
			j.Error = err.Error()
		} else {
			j.Status = "success"
		}
		j.mu.Unlock()
		if m.next != nil {
			payload := map[string]any{"id": j.ID, "name": j.Name, "status": j.Status}
			if err != nil {
				payload["error"] = err.Error()
			}
			m.next("job", payload)
		}
	}()
	return j
}

// List summarizes jobs, newest first.
func (m *JobManager) List() []Job {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]Job, 0, len(m.jobs))
	for _, j := range m.jobs {
		out = append(out, Job{ID: j.ID, Name: j.Name, Status: j.Status, Error: j.Error, StartedAt: j.StartedAt, Finished: j.Finished})
	}
	sort.Slice(out, func(i, k int) bool { return out[i].StartedAt.After(out[k].StartedAt) })
	return out
}

// Get returns one job with its full transcript.
func (m *JobManager) Get(id string) (*Job, bool) {
	m.mu.Lock()
	j, ok := m.jobs[id]
	m.mu.Unlock()
	if !ok {
		return nil, false
	}
	return j, true
}

// jobJSON is the API shape of a job with its transcript.
type jobJSON struct {
	*Job
	Output []string `json:"output"`
}

func (a *App) handleListJobs(w http.ResponseWriter, r *http.Request) {
	if a.Jobs == nil {
		writeJSON(w, http.StatusOK, []Job{})
		return
	}
	writeJSON(w, http.StatusOK, a.Jobs.List())
}

func (a *App) handleGetJob(w http.ResponseWriter, r *http.Request) {
	if a.Jobs == nil {
		errJSON(w, fmt.Errorf("jobs unavailable"), http.StatusNotFound)
		return
	}
	j, ok := a.Jobs.Get(chi.URLParam(r, "id"))
	if !ok {
		errJSON(w, fmt.Errorf("job not found"), http.StatusNotFound)
		return
	}
	writeJSON(w, http.StatusOK, jobJSON{Job: *j, Output: j.Output()})
}
