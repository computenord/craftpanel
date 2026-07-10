package mc

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const (
	StateStopped  = "stopped"
	StateStarting = "starting"
	StateRunning  = "running"
	StateStopping = "stopping"

	ringSize        = 2000
	stopGracePeriod = 30 * time.Second

	// Panel-owned control files, kept in the server dir next to server.json so
	// they are invisible to the file manager jail and excluded from backups.
	runFile     = "run.json"
	consoleLog  = "console.log"
	consoleFifo = "console.in"

	maxConsoleLog = 64 << 20
)

// Event is what console subscribers receive: either a log line or a state
// change of the underlying process.
type Event struct {
	Type   string `json:"t"` // "line" or "status"
	Line   string `json:"line,omitempty"`
	Status string `json:"status,omitempty"`
}

// Proc supervises a single Minecraft server process. On Linux the process
// writes its console to a log file and reads commands from a FIFO, so a
// restarted panel (self update, crash) can reattach to a still running
// server. On Windows (development only) classic pipes are used.
type Proc struct {
	mu            sync.Mutex
	ctlDir        string // server dir holding run.json / console files
	dir           string // working directory of the server process
	state         string
	pid           int
	startedAt     time.Time
	stopRequested bool
	readyMarker   string
	exited        chan struct{}
	exitHook      func(crashed bool, uptime time.Duration)

	cmd      *exec.Cmd // set when this panel started the process itself
	stdin    io.WriteCloser
	tailStop chan struct{}
	tailDone chan struct{}

	ring      []string
	ringNext  int
	ringCount int
	subs      map[chan Event]struct{}
}

func NewProc(ctlDir, dataDir string) *Proc {
	return &Proc{
		ctlDir: ctlDir,
		dir:    dataDir,
		state:  StateStopped,
		ring:   make([]string, ringSize),
		subs:   map[chan Event]struct{}{},
	}
}

func (p *Proc) State() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.state
}

func (p *Proc) Uptime() time.Duration {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.state == StateStopped || p.startedAt.IsZero() {
		return 0
	}
	return time.Since(p.startedAt)
}

// SetExitHook registers a callback fired whenever the process exits. crashed
// is true when nobody asked the server to stop.
func (p *Proc) SetExitHook(hook func(crashed bool, uptime time.Duration)) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.exitHook = hook
}

// PID returns the server process id, or 0 when nothing is running.
func (p *Proc) PID() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.state == StateStopped {
		return 0
	}
	return p.pid
}

// Note writes a panel-generated line into the console stream.
func (p *Proc) Note(line string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.appendLineLocked("[craftpanel] " + line)
}

// WaitForLine blocks until a console line containing substr appears or the
// timeout elapses.
func (p *Proc) WaitForLine(substr string, timeout time.Duration) bool {
	_, ch, cancel := p.Subscribe()
	defer cancel()
	deadline := time.After(timeout)
	for {
		select {
		case ev := <-ch:
			if ev.Type == "line" && strings.Contains(ev.Line, substr) {
				return true
			}
			if ev.Type == "status" && ev.Status == StateStopped {
				return false
			}
		case <-deadline:
			return false
		}
	}
}

// SendCommand writes a single command line to the server console.
func (p *Proc) SendCommand(command string) error {
	command = strings.TrimSpace(strings.ReplaceAll(strings.ReplaceAll(command, "\r", " "), "\n", " "))
	if command == "" {
		return errors.New("empty command")
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.state != StateRunning && p.state != StateStarting {
		return errors.New("server is not running")
	}
	if p.stdin == nil {
		return errors.New("console input is not attached, restart the server")
	}
	p.appendLineLocked("> " + command)
	_, err := io.WriteString(p.stdin, command+"\n")
	return err
}

// Stop asks the server to shut down gracefully and kills it after a grace
// period. It returns once the process has exited.
func (p *Proc) Stop() error {
	p.mu.Lock()
	if p.state == StateStopped {
		p.mu.Unlock()
		return nil
	}
	if p.state == StateStopping {
		exited := p.exited
		p.mu.Unlock()
		<-exited
		return nil
	}
	p.stopRequested = true
	p.setStateLocked(StateStopping)
	p.appendLineLocked("[craftpanel] Stopping server")
	if p.stdin != nil {
		_, _ = io.WriteString(p.stdin, "stop\n")
	}
	exited := p.exited
	p.mu.Unlock()

	select {
	case <-exited:
		return nil
	case <-time.After(stopGracePeriod):
		p.killProcess()
		<-exited
		return nil
	}
}

// Kill terminates the process immediately.
func (p *Proc) Kill() error {
	p.mu.Lock()
	if p.state == StateStopped {
		p.mu.Unlock()
		return nil
	}
	p.stopRequested = true
	exited := p.exited
	p.appendLineLocked("[craftpanel] Killing server process")
	p.mu.Unlock()
	p.killProcess()
	<-exited
	return nil
}

// Subscribe returns buffered history plus a live event channel. Call the
// returned cancel function to unsubscribe.
func (p *Proc) Subscribe() (history []string, ch chan Event, cancel func()) {
	p.mu.Lock()
	defer p.mu.Unlock()
	history = p.historyLocked()
	ch = make(chan Event, 256)
	p.subs[ch] = struct{}{}
	return history, ch, func() {
		p.mu.Lock()
		defer p.mu.Unlock()
		delete(p.subs, ch)
	}
}

func (p *Proc) History() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.historyLocked()
}

func (p *Proc) historyLocked() []string {
	out := make([]string, 0, p.ringCount)
	start := p.ringNext - p.ringCount
	for i := 0; i < p.ringCount; i++ {
		out = append(out, p.ring[((start+i)%ringSize+ringSize)%ringSize])
	}
	return out
}

// emitLine feeds one console line into the ring buffer and checks readiness.
func (p *Proc) emitLine(line string) {
	p.mu.Lock()
	p.appendLineLocked(line)
	if p.state == StateStarting && p.readyMarker != "" && strings.Contains(line, p.readyMarker) {
		p.setStateLocked(StateRunning)
	}
	p.mu.Unlock()
}

func (p *Proc) appendLineLocked(line string) {
	p.ring[p.ringNext] = line
	p.ringNext = (p.ringNext + 1) % ringSize
	if p.ringCount < ringSize {
		p.ringCount++
	}
	p.broadcastLocked(Event{Type: "line", Line: line})
}

func (p *Proc) setStateLocked(state string) {
	p.state = state
	p.broadcastLocked(Event{Type: "status", Status: state})
}

func (p *Proc) broadcastLocked(ev Event) {
	for ch := range p.subs {
		select {
		case ch <- ev:
		default:
			// Slow subscriber: drop the event rather than blocking the server.
		}
	}
}

// finish is the single exit path for both owned and adopted processes.
func (p *Proc) finish(exitMsg string) {
	if p.tailStop != nil {
		close(p.tailStop)
		<-p.tailDone
	}
	p.mu.Lock()
	os.Remove(filepath.Join(p.ctlDir, runFile))
	if p.stdin != nil {
		p.stdin.Close()
		p.stdin = nil
	}
	p.tailStop = nil
	p.tailDone = nil
	p.appendLineLocked(exitMsg)
	p.cmd = nil
	p.pid = 0
	crashed := !p.stopRequested
	uptime := time.Since(p.startedAt)
	hook := p.exitHook
	p.setStateLocked(StateStopped)
	close(p.exited)
	p.mu.Unlock()
	if hook != nil {
		hook(crashed, uptime)
	}
}

func (p *Proc) startTailLocked(path string, offset int64) {
	stop := make(chan struct{})
	done := make(chan struct{})
	p.tailStop = stop
	p.tailDone = done
	go p.tailLoop(path, offset, stop, done)
}

// tailLoop follows the server's console log file, surviving truncation and
// rotating it when it grows too large.
func (p *Proc) tailLoop(path string, offset int64, stop, done chan struct{}) {
	defer close(done)
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()
	if offset > 0 {
		f.Seek(offset, io.SeekStart)
	}
	buf := make([]byte, 32<<10)
	var partial []byte

	read := func() bool {
		n, _ := f.Read(buf)
		if n <= 0 {
			return false
		}
		data := buf[:n]
		for {
			i := bytes.IndexByte(data, '\n')
			if i < 0 {
				break
			}
			line := string(append(partial, data[:i]...))
			partial = partial[:0]
			p.emitLine(strings.TrimRight(line, "\r"))
			data = data[i+1:]
		}
		partial = append(partial, data...)
		return true
	}

	for {
		if read() {
			continue
		}
		select {
		case <-stop:
			read() // final drain after process exit
			if len(partial) > 0 {
				p.emitLine(strings.TrimRight(string(partial), "\r"))
			}
			return
		case <-time.After(250 * time.Millisecond):
		}
		if fi, err := f.Stat(); err == nil {
			if cur, _ := f.Seek(0, io.SeekCurrent); cur > fi.Size() {
				// File was truncated behind our back, start over.
				f.Seek(0, io.SeekStart)
				partial = partial[:0]
			} else if fi.Size() > maxConsoleLog {
				os.Truncate(path, 0)
				f.Seek(0, io.SeekStart)
				partial = partial[:0]
				p.Note("Console log rotated")
			}
		}
	}
}

func startCommandLine(bin string, args []string) string {
	return fmt.Sprintf("[craftpanel] Starting server (%s %s)", bin, strings.Join(args, " "))
}
