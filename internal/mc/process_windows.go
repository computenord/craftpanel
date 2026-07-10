//go:build windows

package mc

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
	"time"
)

// Start on Windows uses classic pipes. Windows is a development platform
// only; processes do not survive panel restarts here and TryAdopt is a no-op.
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
		return fmt.Errorf("start server process: %w", err)
	}

	p.cmd = cmd
	p.pid = cmd.Process.Pid
	p.stdin = stdin
	p.stopRequested = false
	p.readyMarker = readyMarker
	p.exited = make(chan struct{})
	p.startedAt = time.Now()
	p.setStateLocked(StateStarting)
	p.appendLineLocked(startCommandLine(bin, args))

	var readers sync.WaitGroup
	readers.Add(2)
	go p.readLoop(stdout, &readers)
	go p.readLoop(stderr, &readers)

	go func() {
		readers.Wait()
		err := cmd.Wait()
		msg := "[craftpanel] Server process exited"
		if err != nil {
			msg = fmt.Sprintf("[craftpanel] Server process exited: %v", err)
		}
		p.finish(msg)
	}()
	return nil
}

// readLoop drains one of the child's output pipes. It must never stop early:
// an abandoned pipe fills its kernel buffer and blocks the server forever.
// Overlong lines are truncated, not treated as an error.
func (p *Proc) readLoop(r io.Reader, wg *sync.WaitGroup) {
	defer wg.Done()
	const maxLine = 1 << 20
	br := bufio.NewReaderSize(r, 64<<10)
	var buf []byte
	for {
		chunk, isPrefix, err := br.ReadLine()
		if len(chunk) > 0 && len(buf) < maxLine {
			buf = append(buf, chunk...)
		}
		if err != nil {
			if len(buf) > 0 {
				p.emitLine(string(buf))
			}
			return
		}
		if isPrefix {
			continue
		}
		p.emitLine(string(buf))
		buf = buf[:0]
	}
}

// TryAdopt never succeeds on Windows.
func (p *Proc) TryAdopt() bool { return false }

func (p *Proc) killProcess() {
	p.mu.Lock()
	cmd := p.cmd
	p.mu.Unlock()
	if cmd != nil && cmd.Process != nil {
		cmd.Process.Kill()
	}
}
