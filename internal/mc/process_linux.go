//go:build linux

package mc

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/computenord/craftpanel/internal/fsutil"
)

// runState is persisted next to the server so a restarted panel can reattach
// to a process it did not start itself.
type runState struct {
	PID       int       `json:"pid"`
	ProcStart uint64    `json:"procStart"` // /proc start ticks, guards pid reuse
	StartedAt time.Time `json:"startedAt"`
	Marker    string    `json:"marker,omitempty"`
	StopCmd   string    `json:"stopCmd,omitempty"`
}

// Start launches the server process. Its console goes to a log file and its
// stdin comes from a FIFO, so the process survives panel restarts: nothing it
// holds is a pipe into the panel.
func (p *Proc) Start(bin string, args, extraEnv []string, readyMarker string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.state != StateStopped {
		return fmt.Errorf("server is %s", p.state)
	}

	fifoPath := filepath.Join(p.ctlDir, consoleFifo)
	logPath := filepath.Join(p.ctlDir, consoleLog)
	os.Remove(fifoPath)
	if err := syscall.Mkfifo(fifoPath, 0o600); err != nil {
		return fmt.Errorf("create console pipe: %w", err)
	}
	// Open order matters: a blocking O_RDONLY open would wait for a writer,
	// so the read end opens nonblocking first and is switched back after.
	rd, err := os.OpenFile(fifoPath, os.O_RDONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		return fmt.Errorf("open console pipe: %w", err)
	}
	wr, err := os.OpenFile(fifoPath, os.O_WRONLY, 0)
	if err != nil {
		rd.Close()
		return fmt.Errorf("open console pipe writer: %w", err)
	}
	syscall.SetNonblock(int(rd.Fd()), false)

	logF, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC|os.O_APPEND, 0o640)
	if err != nil {
		rd.Close()
		wr.Close()
		return fmt.Errorf("open console log: %w", err)
	}

	cmd := exec.Command(bin, args...)
	cmd.Dir = p.dir
	if len(extraEnv) > 0 {
		cmd.Env = append(os.Environ(), extraEnv...)
	}
	cmd.Stdin = rd
	cmd.Stdout = logF
	cmd.Stderr = logF
	// The child inherits a write end of its own stdin FIFO (fd 3, unused by
	// it). That keeps the FIFO from ever reporting EOF, so a restarted panel
	// can reopen its writer and keep sending commands.
	cmd.ExtraFiles = []*os.File{wr}
	if err := cmd.Start(); err != nil {
		rd.Close()
		wr.Close()
		logF.Close()
		os.Remove(fifoPath)
		return fmt.Errorf("start server process: %w", err)
	}
	rd.Close()
	logF.Close()

	p.cmd = cmd
	p.pid = cmd.Process.Pid
	p.stdin = wr
	p.stopRequested = false
	p.readyMarker = readyMarker
	p.exited = make(chan struct{})
	p.startedAt = time.Now()
	p.setStateLocked(StateStarting)
	p.appendLineLocked(startCommandLine(bin, args))

	rs := runState{PID: p.pid, ProcStart: procStartTicks(p.pid), StartedAt: p.startedAt, Marker: readyMarker, StopCmd: p.stopCommand}
	if err := fsutil.WriteJSONAtomic(filepath.Join(p.ctlDir, runFile), rs); err != nil {
		p.appendLineLocked("[craftpanel] Warning: could not persist run state: " + err.Error())
	}
	p.startTailLocked(logPath, 0)

	go func() {
		err := cmd.Wait()
		msg := "[craftpanel] Server process exited"
		if err != nil {
			msg = fmt.Sprintf("[craftpanel] Server process exited: %v", err)
		}
		p.finish(msg)
	}()
	return nil
}

// TryAdopt reattaches to a server process left running by a previous panel
// instance (self update or panel crash). Returns true when a live process was
// adopted.
func (p *Proc) TryAdopt() bool {
	runPath := filepath.Join(p.ctlDir, runFile)
	var rs runState
	if err := fsutil.ReadJSON(runPath, &rs); err != nil || rs.PID <= 0 {
		return false
	}
	if ticks := procStartTicks(rs.PID); ticks == 0 || ticks != rs.ProcStart {
		os.Remove(runPath) // process is gone, or the pid was reused
		return false
	}

	p.mu.Lock()
	fifoPath := filepath.Join(p.ctlDir, consoleFifo)
	// The child still holds both FIFO ends, so this open succeeds instantly.
	if wr, err := os.OpenFile(fifoPath, os.O_WRONLY|syscall.O_NONBLOCK, 0); err == nil {
		syscall.SetNonblock(int(wr.Fd()), false)
		p.stdin = wr
	}
	p.pid = rs.PID
	p.startedAt = rs.StartedAt
	p.readyMarker = rs.Marker
	p.stopCommand = rs.StopCmd
	p.stopRequested = false
	p.exited = make(chan struct{})
	p.setStateLocked(StateRunning)
	p.appendLineLocked(fmt.Sprintf("[craftpanel] Reattached to running server (pid %d)", rs.PID))

	logPath := filepath.Join(p.ctlDir, consoleLog)
	var offset int64
	if fi, err := os.Stat(logPath); err == nil && fi.Size() > 64<<10 {
		offset = fi.Size() - 64<<10
	}
	p.startTailLocked(logPath, offset)
	p.mu.Unlock()

	// Not our child, so no Wait(): poll for process exit instead.
	go func() {
		for {
			time.Sleep(time.Second)
			if err := syscall.Kill(rs.PID, 0); err != nil && errors.Is(err, syscall.ESRCH) {
				break
			}
		}
		p.finish("[craftpanel] Server process exited")
	}()
	return true
}

func (p *Proc) killProcess() {
	p.mu.Lock()
	pid := p.pid
	p.mu.Unlock()
	if pid > 0 {
		syscall.Kill(pid, syscall.SIGKILL)
	}
}

// procStartTicks reads a process's start time from /proc, used to tell a live
// process from a reused pid.
func procStartTicks(pid int) uint64 {
	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return 0
	}
	s := string(data)
	i := strings.LastIndexByte(s, ')')
	if i < 0 || i+2 >= len(s) {
		return 0
	}
	fields := strings.Fields(s[i+2:])
	if len(fields) < 20 {
		return 0
	}
	v, _ := strconv.ParseUint(fields[19], 10, 64)
	return v
}
