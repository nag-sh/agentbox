package init

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"sync"
	"syscall"
	"time"

	"github.com/charmbracelet/log"
)

// Supervisor is a lightweight process supervisor for MCP servers running
// inside an agentbox container. It manages process lifecycles including
// startup, health checking, automatic restart with backoff, and graceful
// shutdown.
//
// The supervisor is designed to be simple and reliable — it is not a
// general-purpose process manager, but rather a focused tool for managing
// the small number of MCP server processes that an agentbox container
// typically runs.
type Supervisor struct {
	// mu protects the processes map.
	mu sync.Mutex

	// processes maps process names to their managed state.
	processes map[string]*managedState

	// logger is the structured logger for supervisor operations.
	logger *log.Logger
}

// ManagedProcess describes a process to be started and supervised.
type ManagedProcess struct {
	// Name is the human-readable process name, used for logging and status.
	Name string

	// Command is the path to the executable.
	Command string

	// Args are the command-line arguments.
	Args []string

	// Env are environment variables specific to this process.
	// These are merged with the inherited environment.
	Env map[string]string

	// HealthCheck describes how to verify the process is ready.
	// If nil, the process is considered healthy as soon as it starts.
	HealthCheck *HealthCheck

	// MaxRestarts is the maximum number of automatic restarts on failure.
	// Zero means no automatic restarts. Use -1 for unlimited restarts.
	MaxRestarts int
}

// managedState holds the runtime state of a supervised process.
type managedState struct {
	proc      *ManagedProcess
	cmd       *exec.Cmd
	pid       int
	state     string
	restarts  int
	lastError error
	exitCode  int
	cancel    context.CancelFunc
	done      chan struct{}

	// stdout and stderr capture buffers for debugging.
	stdout bytes.Buffer
	stderr bytes.Buffer
}

// ProcessStatus represents a snapshot of a managed process's current state.
// Returned by [Supervisor.Status] for observability.
type ProcessStatus struct {
	// Name is the process name.
	Name string

	// PID is the OS process ID. Zero if the process is not running.
	PID int

	// State is the current state: "starting", "running", "healthy",
	// "unhealthy", "stopped", or "failed".
	State string

	// Restarts is the number of times this process has been restarted.
	Restarts int

	// LastError is the most recent error, if any.
	LastError error

	// ExitCode is the last exit code. -1 if the process has not exited.
	ExitCode int
}

// backoffSchedule defines the restart backoff intervals. Each successive
// restart waits longer before attempting to restart the process.
var backoffSchedule = []time.Duration{
	1 * time.Second,
	2 * time.Second,
	5 * time.Second,
	10 * time.Second,
	30 * time.Second,
}

// NewSupervisor creates a new process supervisor.
func NewSupervisor(logger *log.Logger) *Supervisor {
	return &Supervisor{
		processes: make(map[string]*managedState),
		logger:    logger,
	}
}

// Start launches a managed process and begins supervising it. If the process
// has a health check configured, it will be monitored in a background goroutine.
// If the process exits unexpectedly and MaxRestarts has not been exceeded, it
// will be automatically restarted with exponential backoff.
//
// Start returns an error if the process cannot be started initially. Subsequent
// restart failures are logged but do not surface as errors — check
// [Supervisor.Status] for current state.
func (s *Supervisor) Start(ctx context.Context, proc *ManagedProcess) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.processes[proc.Name]; exists {
		return fmt.Errorf("process %q already managed", proc.Name)
	}

	state, err := s.startProcess(ctx, proc)
	if err != nil {
		return fmt.Errorf("starting %s: %w", proc.Name, err)
	}

	s.processes[proc.Name] = state
	s.logger.Info("process started", "name", proc.Name, "pid", state.pid)

	// Launch supervision goroutine.
	go s.supervise(ctx, state)

	return nil
}

// StopAll initiates graceful shutdown of all managed processes. Each process
// receives SIGTERM and is given until the timeout to exit. If a process does
// not exit within the timeout, it is forcefully killed with SIGKILL.
//
// StopAll blocks until all processes have exited or been killed.
func (s *Supervisor) StopAll(ctx context.Context, timeout time.Duration) error {
	s.mu.Lock()
	states := make([]*managedState, 0, len(s.processes))
	for _, st := range s.processes {
		states = append(states, st)
	}
	s.mu.Unlock()

	if len(states) == 0 {
		return nil
	}

	s.logger.Info("stopping all processes", "count", len(states), "timeout", timeout)

	var errs []error

	// Send SIGTERM to all processes.
	for _, st := range states {
		if st.cmd != nil && st.cmd.Process != nil {
			s.logger.Debug("sending SIGTERM", "name", st.proc.Name, "pid", st.pid)
			if err := st.cmd.Process.Signal(syscall.SIGTERM); err != nil {
				s.logger.Debug("SIGTERM failed", "name", st.proc.Name, "error", err)
			}
		}
		// Cancel the supervision context to prevent restarts.
		if st.cancel != nil {
			st.cancel()
		}
	}

	// Wait for all processes to exit, with timeout.
	deadline := time.After(timeout)
	for _, st := range states {
		select {
		case <-st.done:
			s.logger.Debug("process exited gracefully", "name", st.proc.Name)
		case <-deadline:
			// Force kill remaining processes.
			s.logger.Warn("process did not exit in time, sending SIGKILL",
				"name", st.proc.Name, "pid", st.pid)
			if st.cmd != nil && st.cmd.Process != nil {
				if err := st.cmd.Process.Kill(); err != nil {
					errs = append(errs, fmt.Errorf("killing %s: %w", st.proc.Name, err))
				}
			}
		case <-ctx.Done():
			errs = append(errs, ctx.Err())
			return errors.Join(errs...)
		}
	}

	return errors.Join(errs...)
}

// WaitHealthy blocks until all managed processes with health checks have
// passed their health checks, or until the timeout expires. Processes
// without health checks are considered immediately healthy.
//
// Returns an error if the timeout expires before all processes are healthy,
// or if any process enters the "failed" state.
func (s *Supervisor) WaitHealthy(ctx context.Context, timeout time.Duration) error {
	deadline, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-deadline.Done():
			// Build a status report of unhealthy processes.
			statuses := s.Status()
			var unhealthy []string
			for name, status := range statuses {
				if status.State != "healthy" && status.State != "running" {
					unhealthy = append(unhealthy, fmt.Sprintf("%s (%s)", name, status.State))
				}
			}
			if len(unhealthy) > 0 {
				return fmt.Errorf("health check timeout: unhealthy processes: %s",
					fmt.Sprintf("%v", unhealthy))
			}
			return deadline.Err()

		case <-ticker.C:
			if s.allHealthy() {
				return nil
			}
		}
	}
}

// Status returns a snapshot of the current state of all managed processes.
// The returned map is keyed by process name.
func (s *Supervisor) Status() map[string]ProcessStatus {
	s.mu.Lock()
	defer s.mu.Unlock()

	statuses := make(map[string]ProcessStatus, len(s.processes))
	for name, st := range s.processes {
		statuses[name] = ProcessStatus{
			Name:      st.proc.Name,
			PID:       st.pid,
			State:     st.state,
			Restarts:  st.restarts,
			LastError: st.lastError,
			ExitCode:  st.exitCode,
		}
	}
	return statuses
}

// startProcess creates and starts an OS process for the given ManagedProcess.
func (s *Supervisor) startProcess(ctx context.Context, proc *ManagedProcess) (*managedState, error) {
	procCtx, cancel := context.WithCancel(ctx)

	cmd := exec.CommandContext(procCtx, proc.Command, proc.Args...)

	// Build environment: inherit current env + process-specific vars.
	cmd.Env = os.Environ()
	for k, v := range proc.Env {
		cmd.Env = append(cmd.Env, k+"="+v)
	}

	state := &managedState{
		proc:     proc,
		cmd:      cmd,
		state:    "starting",
		exitCode: -1,
		cancel:   cancel,
		done:     make(chan struct{}),
	}

	// Capture stdout/stderr for debugging.
	cmd.Stdout = &state.stdout
	cmd.Stderr = &state.stderr

	if err := cmd.Start(); err != nil {
		cancel()
		return nil, fmt.Errorf("exec %s: %w", proc.Command, err)
	}

	state.pid = cmd.Process.Pid
	state.state = "running"

	return state, nil
}

// supervise monitors a running process, handles health checking, and
// performs automatic restarts with backoff on failure.
func (s *Supervisor) supervise(ctx context.Context, st *managedState) {
	defer close(st.done)

	// Start health check loop if configured.
	if st.proc.HealthCheck != nil {
		go s.healthCheckLoop(ctx, st)
	} else {
		// No health check: immediately consider healthy.
		s.mu.Lock()
		st.state = "healthy"
		s.mu.Unlock()
	}

	for {
		// Wait for process to exit.
		err := st.cmd.Wait()

		s.mu.Lock()
		if err != nil {
			st.lastError = err
			if exitErr, ok := err.(*exec.ExitError); ok {
				st.exitCode = exitErr.ExitCode()
			}
			s.logger.Error("process exited with error",
				"name", st.proc.Name,
				"pid", st.pid,
				"exit_code", st.exitCode,
				"error", err,
			)
		} else {
			st.exitCode = 0
			s.logger.Info("process exited normally", "name", st.proc.Name, "pid", st.pid)
		}

		// Check if we should restart.
		maxRestarts := st.proc.MaxRestarts
		if maxRestarts == 0 || (maxRestarts > 0 && st.restarts >= maxRestarts) {
			st.state = "stopped"
			if err != nil {
				st.state = "failed"
			}
			s.mu.Unlock()
			return
		}

		st.restarts++
		restartCount := st.restarts
		s.mu.Unlock()

		// Check context cancellation (shutdown in progress).
		if ctx.Err() != nil {
			s.mu.Lock()
			st.state = "stopped"
			s.mu.Unlock()
			return
		}

		// Backoff before restart.
		backoff := backoffDuration(restartCount)
		s.logger.Info("restarting process",
			"name", st.proc.Name,
			"restart", restartCount,
			"backoff", backoff,
		)

		select {
		case <-time.After(backoff):
		case <-ctx.Done():
			s.mu.Lock()
			st.state = "stopped"
			s.mu.Unlock()
			return
		}

		// Restart the process.
		newState, err := s.startProcess(ctx, st.proc)
		if err != nil {
			s.logger.Error("restart failed", "name", st.proc.Name, "error", err)
			s.mu.Lock()
			st.state = "failed"
			st.lastError = err
			s.mu.Unlock()
			return
		}

		// Update state with new process info.
		s.mu.Lock()
		st.cmd = newState.cmd
		st.pid = newState.pid
		st.state = "running"
		st.stdout = newState.stdout
		st.stderr = newState.stderr
		s.mu.Unlock()

		// Re-launch health check if configured.
		if st.proc.HealthCheck != nil {
			go s.healthCheckLoop(ctx, st)
		} else {
			s.mu.Lock()
			st.state = "healthy"
			s.mu.Unlock()
		}
	}
}

// healthCheckLoop periodically runs health checks on a managed process
// and updates its state accordingly.
func (s *Supervisor) healthCheckLoop(ctx context.Context, st *managedState) {
	hc := st.proc.HealthCheck
	if hc == nil {
		return
	}

	interval := hc.Interval
	if interval == 0 {
		interval = 5 * time.Second
	}
	timeout := hc.Timeout
	if timeout == 0 {
		timeout = 3 * time.Second
	}
	maxRetries := hc.Retries
	if maxRetries == 0 {
		maxRetries = 3
	}

	// Wait for start period before beginning health checks.
	if hc.StartPeriod > 0 {
		select {
		case <-time.After(hc.StartPeriod):
		case <-ctx.Done():
			return
		}
	}

	consecutiveFailures := 0

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-st.done:
			return
		case <-ticker.C:
			if err := s.runHealthCheck(ctx, st, timeout); err != nil {
				consecutiveFailures++
				s.logger.Debug("health check failed",
					"name", st.proc.Name,
					"failures", consecutiveFailures,
					"max", maxRetries,
					"error", err,
				)
				if consecutiveFailures >= maxRetries {
					s.mu.Lock()
					st.state = "unhealthy"
					st.lastError = fmt.Errorf("health check failed %d times: %w", consecutiveFailures, err)
					s.mu.Unlock()
				}
			} else {
				consecutiveFailures = 0
				s.mu.Lock()
				if st.state == "running" || st.state == "unhealthy" {
					st.state = "healthy"
				}
				s.mu.Unlock()
			}
		}
	}
}

// runHealthCheck executes a single health check command with a timeout.
func (s *Supervisor) runHealthCheck(ctx context.Context, st *managedState, timeout time.Duration) error {
	hc := st.proc.HealthCheck
	if len(hc.Command) == 0 {
		return nil
	}

	checkCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(checkCtx, hc.Command[0], hc.Command[1:]...)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("health check command failed: %w", err)
	}
	return nil
}

// allHealthy returns true if all managed processes are in a healthy state.
// Processes without health checks that are running are considered healthy.
func (s *Supervisor) allHealthy() bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, st := range s.processes {
		if st.state != "healthy" {
			return false
		}
	}
	return true
}

// backoffDuration returns the backoff duration for the given restart attempt.
// Uses the backoffSchedule with a cap at the last entry.
func backoffDuration(attempt int) time.Duration {
	if attempt <= 0 {
		return backoffSchedule[0]
	}
	idx := attempt - 1
	if idx >= len(backoffSchedule) {
		idx = len(backoffSchedule) - 1
	}
	return backoffSchedule[idx]
}
