package mc

import (
	"regexp"
	"strconv"
	"time"
)

// TPS sampling: Paper-family servers answer the `tps` console command with a
// line like "TPS from last 1m, 5m, 15m: 20.0, 20.0, 20.0" (values may carry a
// leading '*' when clamped). The sampler injects the command quietly and the
// response line is captured before it reaches the visible console, so the
// stream stays clean.

var tpsLineRe = regexp.MustCompile(`TPS from last[^:]*:\s*\*?([0-9]+(?:\.[0-9]+)?)`)

const tpsRefresh = 15 * time.Second

// SupportsTPS reports whether the server type answers the `tps` console
// command in the Paper format.
func SupportsTPS(typ string) bool {
	return typ == TypePaper || typ == TypePurpur
}

// cachedTPSLocked returns the last sampled TPS (1m average) and refreshes it
// in the background when stale. Never blocks the API. Callers hold s.mu.
func (s *Server) cachedTPSLocked() float64 {
	if time.Since(s.tpsAt) > tpsRefresh && !s.tpsBusy {
		s.tpsBusy = true
		proc := s.proc
		go func() {
			tps, ok := queryTPS(proc)
			s.mu.Lock()
			if ok {
				s.tps = tps
			} else {
				s.tps = 0
			}
			s.tpsAt = time.Now()
			s.tpsBusy = false
			s.mu.Unlock()
		}()
	}
	return s.tps
}

func queryTPS(p *Proc) (float64, bool) {
	line, err := p.QueryQuiet("tps", tpsLineRe, 3*time.Second)
	if err != nil {
		return 0, false
	}
	m := tpsLineRe.FindStringSubmatch(line)
	if m == nil {
		return 0, false
	}
	v, err := strconv.ParseFloat(m[1], 64)
	if err != nil || v <= 0 {
		return 0, false
	}
	// Clamp the vanilla ceiling; Paper can report slightly above 20.
	if v > 20 {
		v = 20
	}
	return v, true
}
