package web

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/sceptyre/maia/internal/llm"
)

// JobEvent is a single streamed item: a log line, a status change, or the end marker.
type JobEvent struct {
	ID   int    `json:"id"`
	Type string `json:"type"` // "line" | "status" | "end"
	Data string `json:"data"`
}

// Job is one command execution (init, plan, apply, ..., issue import, PR).
type Job struct {
	ID        string    `json:"id"`
	ProjectID string    `json:"project_id"`
	Slug      string    `json:"slug"`
	Kind      string    `json:"kind"`
	Command   string    `json:"command"`
	Status    string    `json:"status"` // running | ok | error | canceled
	ExitCode  int       `json:"exit_code"`
	Started   time.Time `json:"started"`
	Ended     time.Time `json:"ended,omitempty"`

	mu       sync.Mutex
	nextID   int
	events   []JobEvent
	subs     map[chan JobEvent]struct{}
	proc     *os.Process
	canceled bool
	done     bool
	doneCh   chan struct{}
}

// Done is closed when the job reaches a terminal status.
func (j *Job) Done() <-chan struct{} {
	return j.doneCh
}

const maxJobEvents = 20000

func (j *Job) append(typ, data string) {
	j.mu.Lock()
	j.nextID++
	ev := JobEvent{ID: j.nextID, Type: typ, Data: data}
	j.events = append(j.events, ev)
	if len(j.events) > maxJobEvents {
		j.events = j.events[len(j.events)-maxJobEvents:]
	}
	subs := make([]chan JobEvent, 0, len(j.subs))
	for ch := range j.subs {
		subs = append(subs, ch)
	}
	j.mu.Unlock()

	for _, ch := range subs {
		select {
		case ch <- ev:
		default:
			// Slow subscriber: drop; it can recover via replay on reconnect.
		}
	}
}

// snapshot registers a subscriber and returns events with ID > after, atomically.
func (j *Job) snapshot(after int) ([]JobEvent, chan JobEvent, func()) {
	j.mu.Lock()
	defer j.mu.Unlock()

	var replay []JobEvent
	for _, ev := range j.events {
		if ev.ID > after {
			replay = append(replay, ev)
		}
	}
	ch := make(chan JobEvent, 1024)
	j.subs[ch] = struct{}{}
	unsubscribe := func() {
		j.mu.Lock()
		delete(j.subs, ch)
		j.mu.Unlock()
	}
	return replay, ch, unsubscribe
}

func (j *Job) finish(status string, exitCode int) {
	j.mu.Lock()
	if j.done {
		j.mu.Unlock()
		return
	}
	j.done = true
	j.Status = status
	j.ExitCode = exitCode
	j.Ended = time.Now()
	close(j.doneCh)
	end := JobEvent{ID: -1, Type: "end", Data: status}
	subs := make([]chan JobEvent, 0, len(j.subs))
	for ch := range j.subs {
		subs = append(subs, ch)
	}
	j.mu.Unlock()

	for _, ch := range subs {
		select {
		case ch <- end:
		default:
		}
	}
}

// JobSummary is a job without its event log.
type JobSummary struct {
	ID        string    `json:"id"`
	ProjectID string    `json:"project_id"`
	Slug      string    `json:"slug"`
	Kind      string    `json:"kind"`
	Command   string    `json:"command"`
	Status    string    `json:"status"`
	ExitCode  int       `json:"exit_code"`
	Started   time.Time `json:"started"`
	Ended     time.Time `json:"ended,omitempty"`
	Lines     int       `json:"lines"`
}

func (j *Job) summary() JobSummary {
	j.mu.Lock()
	defer j.mu.Unlock()
	return JobSummary{
		ID: j.ID, ProjectID: j.ProjectID, Slug: j.Slug, Kind: j.Kind, Command: j.Command,
		Status: j.Status, ExitCode: j.ExitCode, Started: j.Started, Ended: j.Ended,
		Lines: len(j.events),
	}
}

func (j *Job) isDone() bool {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.done
}

// JobManager allows one command at a time per change request and keeps job history.
type JobManager struct {
	mu             sync.Mutex
	jobs           map[string]*Job
	order          []string // job IDs, oldest first
	runningByScope map[string]string
	seq            int
}

func NewJobManager() *JobManager {
	return &JobManager{jobs: make(map[string]*Job), runningByScope: make(map[string]string)}
}

// JobStart describes a command to run as a job.
type JobStart struct {
	ProjectID string
	Slug      string
	Kind      string
	Command   string // display name (e.g. "init", "pr create")
	Dir       string // working directory
	Exe       string // "" = the maia binary itself
	Argv      []string
}

// Start launches the command described by opts. The maia binary is prepended
// unless opts.Exe names another program (e.g. "gh").
func (m *JobManager) Start(opts JobStart) (*Job, error) {
	scope := opts.ProjectID + "\x00" + opts.Slug
	if opts.Slug == "" {
		scope += "\x00" + opts.Command
	}
	m.mu.Lock()
	if id := m.runningByScope[scope]; id != "" {
		cur := m.jobs[id]
		m.mu.Unlock()
		return nil, fmt.Errorf("a command is already running for this change request: %s (%s)", cur.Command, cur.Slug)
	}

	m.seq++
	job := &Job{
		ID:        fmt.Sprintf("job-%d", m.seq),
		ProjectID: opts.ProjectID,
		Slug:      opts.Slug,
		Kind:      opts.Kind,
		Command:   opts.Command,
		Status:    "running",
		Started:   time.Now(),
		subs:      make(map[chan JobEvent]struct{}),
		doneCh:    make(chan struct{}),
	}
	m.jobs[job.ID] = job
	m.order = append(m.order, job.ID)
	m.runningByScope[scope] = job.ID
	m.pruneLocked()
	m.mu.Unlock()

	fail := func(err error) (*Job, error) {
		m.clearRunning(scope, job.ID)
		job.finish("error", 1)
		return nil, err
	}

	exe := opts.Exe
	if exe == "" {
		self, err := os.Executable()
		if err != nil {
			return fail(fmt.Errorf("cannot locate maia binary: %w", err))
		}
		exe = self
	}

	cmd := exec.Command(exe, opts.Argv...)
	cmd.Dir = opts.Dir
	work := opts.Slug
	if work == "" {
		work = opts.Command
	}
	for _, env := range os.Environ() {
		if !strings.HasPrefix(env, "MAIA_SESSION_ID=") {
			cmd.Env = append(cmd.Env, env)
		}
	}
	cmd.Env = append(cmd.Env, "MAIA_SESSION_ID="+llm.SessionIDFor(opts.Dir, work))

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fail(err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return fail(err)
	}

	if err := cmd.Start(); err != nil {
		return fail(fmt.Errorf("failed to start %s: %w", opts.Command, err))
	}

	job.mu.Lock()
	job.proc = cmd.Process
	job.mu.Unlock()

	go pump(stdout, job)
	go pump(stderr, job)

	go func() {
		err := cmd.Wait()
		exitCode := 0
		status := "ok"
		if err != nil {
			exitCode = 1
			if ee, ok := err.(*exec.ExitError); ok {
				exitCode = ee.ExitCode()
			}
			status = "error"
		}
		job.mu.Lock()
		if job.canceled {
			status = "canceled"
		}
		job.mu.Unlock()
		job.finish(status, exitCode)
		m.clearRunning(scope, job.ID)
	}()

	return job, nil
}

func (m *JobManager) clearRunning(scope, id string) {
	m.mu.Lock()
	if m.runningByScope[scope] == id {
		delete(m.runningByScope, scope)
	}
	m.mu.Unlock()
}

func pump(r io.Reader, job *Job) {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		job.append("line", scanner.Text())
	}
}

func (m *JobManager) pruneLocked() {
	const keep = 20
	for len(m.order) > keep {
		removed := false
		for i, id := range m.order {
			job := m.jobs[id]
			if job != nil && !job.isDone() {
				continue
			}
			m.order = append(m.order[:i], m.order[i+1:]...)
			delete(m.jobs, id)
			removed = true
			break
		}
		if !removed {
			return
		}
	}
}

// Get returns a job by ID.
func (m *JobManager) Get(id string) (*Job, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	j, ok := m.jobs[id]
	return j, ok
}

// Running returns the newest running job summary, if any.
func (m *JobManager) Running() (JobSummary, bool) {
	jobs := m.RunningAll()
	if len(jobs) == 0 {
		return JobSummary{}, false
	}
	return jobs[0], true
}

func (m *JobManager) RunningAll() []JobSummary {
	m.mu.Lock()
	defer m.mu.Unlock()
	active := make(map[string]bool, len(m.runningByScope))
	for _, id := range m.runningByScope {
		active[id] = true
	}
	out := make([]JobSummary, 0, len(active))
	for i := len(m.order) - 1; i >= 0; i-- {
		id := m.order[i]
		if active[id] {
			if job := m.jobs[id]; job != nil {
				out = append(out, job.summary())
			}
		}
	}
	return out
}

// List returns all retained jobs, newest first.
func (m *JobManager) List() []JobSummary {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]JobSummary, 0, len(m.order))
	for i := len(m.order) - 1; i >= 0; i-- {
		out = append(out, m.jobs[m.order[i]].summary())
	}
	return out
}

// Cancel kills the running job's process.
func (m *JobManager) Cancel(id string) error {
	j, ok := m.Get(id)
	if !ok {
		return fmt.Errorf("job not found: %s", id)
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.done {
		return nil
	}
	j.canceled = true
	if j.proc != nil {
		return j.proc.Kill()
	}
	return nil
}
