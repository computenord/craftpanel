package mc

import (
	"context"
	"os/exec"
	"regexp"
	"strconv"
	"sync"
	"time"
)

// javaVersionRe matches the quoted version in `java -version` output, which
// looks like `openjdk version "21.0.11" 2026-04-21 LTS` on modern JDKs and
// `java version "1.8.0_202"` on Java 8.
var javaVersionRe = regexp.MustCompile(`version "(\d+)(?:\.(\d+))?`)

// parseJavaMajor extracts the major version from `java -version` output.
// Returns 0 when the output cannot be understood.
func parseJavaMajor(out string) int {
	m := javaVersionRe.FindStringSubmatch(out)
	if m == nil {
		return 0
	}
	first, err := strconv.Atoi(m[1])
	if err != nil {
		return 0
	}
	// Java 8 and older report as 1.8, the real major is the second component.
	if first == 1 && m[2] != "" {
		second, err := strconv.Atoi(m[2])
		if err != nil {
			return 0
		}
		return second
	}
	return first
}

type javaProbe struct {
	major   int
	probed  time.Time
	version string
}

var (
	javaProbeMu    sync.Mutex
	javaProbeCache = map[string]javaProbe{}
)

// DetectJava runs `java -version` and reports the major version and the raw
// first output line. Results are cached per binary path for a minute, since
// this is called on every server start and on the dashboard poll.
func DetectJava(javaPath string) (major int, version string) {
	if javaPath == "" {
		javaPath = "java"
	}
	javaProbeMu.Lock()
	if p, ok := javaProbeCache[javaPath]; ok && time.Since(p.probed) < time.Minute {
		javaProbeMu.Unlock()
		return p.major, p.version
	}
	javaProbeMu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	// `java -version` writes to stderr, so combine both streams.
	out, err := exec.CommandContext(ctx, javaPath, "-version").CombinedOutput()
	if err != nil {
		return 0, ""
	}
	text := string(out)
	major = parseJavaMajor(text)
	version = firstLine(text)

	javaProbeMu.Lock()
	javaProbeCache[javaPath] = javaProbe{major: major, version: version, probed: time.Now()}
	javaProbeMu.Unlock()
	return major, version
}

func firstLine(s string) string {
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' || s[i] == '\r' {
			return s[:i]
		}
	}
	return s
}
