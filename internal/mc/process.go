package mc

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
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
)

// Event is what console subscribers receive: either a log line or a state
// change of the underlying process.
type Event struct {
	Type   string `json:"t"` // "line" or "status"
	Line   string `json:"line,omitempty"`
	Status string `json:"status,omitempty"`
}

// Proc supervises a single Minecraft server process: lifecycle, stdin
// commands and a console ring buffer with live subscribers.
type Proc struct {
	mu            sync.Mutex
	dir           string
	state         string
	cmd           *exec.Cmd
	stdin         io.WriteCloser
	exited        chan struct{}
	startedAt     time.Time
	pid           int
	stopRequested bool
	exitHook      func(crashed bool, uptime time.Duration)
	readyMarker   string

	ring      []string
	ringNext  int
	ringCount int
	subs      map[chan Event]struct{}
}

// SetExitHook registers a callback fired whenever the process exits. crashed
// is true when nobody asked the server to stop.
func (p *Proc) SetExitHook(hook func(crashed bool, uptime time.Duration)) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.exitHook = hook
}

// PID returns the java process id, or 0 when nothing is running.
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

func NewProc(dir string) *Proc {
	return &Proc{
		dir:   dir,
		state: StateStopped,
		ring:  make([]string, ringSize),
		subs:  map[chan Event]struct{}{},
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

// Start launches the server process. readyMarker is the console substring
// that signals the server finished booting.
func (p *Proc) Start(bin string, args, extraEnv []string, readyMarker string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.state != StateStopped {
		return fmt.Errorf("server is %s", p.state)
	}

	cmd := exec.Command(bin, args...)
	cmd.Dir = p.dir
	if len(extraEnv) > 0 {
		cmd.Env = append(os.Environ(), extraEnv...)
	}
	p.readyMarker = readyMarker

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start java: %w", err)
	}

	p.cmd = cmd
	p.stdin = stdin
	p.exited = make(chan struct{})
	p.startedAt = time.Now()
	p.pid = cmd.Process.Pid
	p.stopRequested = false
	p.setStateLocked(StateStarting)
	p.appendLineLocked(fmt.Sprintf("[craftpanel] Starting server (%s %s)", bin, strings.Join(args, " ")))

	var readers sync.WaitGroup
	readers.Add(2)
	go p.readLoop(stdout, &readers)
	go p.readLoop(stderr, &readers)

	exited := p.exited
	go func() {
		readers.Wait()
		err := cmd.Wait()
		p.mu.Lock()
		msg := "[craftpanel] Server process exited"
		if err != nil {
			msg = fmt.Sprintf("[craftpanel] Server process exited: %v", err)
		}
		p.appendLineLocked(msg)
		p.cmd = nil
		p.stdin = nil
		p.pid = 0
		crashed := !p.stopRequested
		uptime := time.Since(p.startedAt)
		hook := p.exitHook
		p.setStateLocked(StateStopped)
		close(exited)
		p.mu.Unlock()
		if hook != nil {
			hook(crashed, uptime)
		}
	}()
	return nil
}

// readLoop drains one of the child's output pipes. It must never stop early:
// an abandoned pipe fills its kernel buffer and blocks the Minecraft server
// forever. Overlong lines are therefore truncated, not treated as an error.
func (p *Proc) readLoop(r io.Reader, wg *sync.WaitGroup) {
	defer wg.Done()
	const maxLine = 1 << 20
	br := bufio.NewReaderSize(r, 64<<10)
	var buf []byte
	emit := func() {
		line := string(buf)
		buf = buf[:0]
		p.mu.Lock()
		p.appendLineLocked(line)
		if p.state == StateStarting && p.readyMarker != "" && strings.Contains(line, p.readyMarker) {
			p.setStateLocked(StateRunning)
		}
		p.mu.Unlock()
	}
	for {
		chunk, isPrefix, err := br.ReadLine()
		if len(chunk) > 0 && len(buf) < maxLine {
			buf = append(buf, chunk...)
		}
		if err != nil {
			if len(buf) > 0 {
				emit()
			}
			return
		}
		if isPrefix {
			continue
		}
		emit()
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
	cmd := p.cmd
	p.mu.Unlock()

	select {
	case <-exited:
		return nil
	case <-time.After(stopGracePeriod):
		if cmd != nil && cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
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
	cmd := p.cmd
	exited := p.exited
	p.appendLineLocked("[craftpanel] Killing server process")
	p.mu.Unlock()
	if cmd != nil && cmd.Process != nil {
		_ = cmd.Process.Kill()
	}
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
