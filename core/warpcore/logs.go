package warpcore

import (
	"bytes"
	"io"
	"log"
	"os"
	"strings"
	"sync"
	"sync/atomic"
)

const maxLogLines = 400

// ringLog keeps the last few hundred log lines for the app's log view and
// watches usque's messages to know when a MASQUE tunnel is actually up.
type ringLog struct {
	mu      sync.Mutex
	lines   []string
	total   int // lines ever written, so callers can look only at newer ones
	partial []byte
	echo    atomic.Bool
}

var (
	logs     = &ringLog{}
	masqueUp atomic.Bool
)

func init() {
	log.SetFlags(log.Ltime)
	log.SetOutput(logs)
}

func (r *ringLog) Write(p []byte) (int, error) {
	if r.echo.Load() {
		_, _ = os.Stderr.Write(p)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.partial = append(r.partial, p...)
	for {
		i := bytes.IndexByte(r.partial, '\n')
		if i < 0 {
			break
		}
		line := string(r.partial[:i])
		r.partial = r.partial[i+1:]
		watchLine(line)
		r.total++
		r.lines = append(r.lines, line)
		if len(r.lines) > maxLogLines {
			r.lines = r.lines[len(r.lines)-maxLogLines:]
		}
	}
	return len(p), nil
}

// watchLine tracks usque's MaintainTunnel messages (api/tunnel.go).
func watchLine(line string) {
	switch {
	case strings.Contains(line, "Connected to MASQUE server"):
		masqueUp.Store(true)
	case strings.Contains(line, "Tunnel connection lost"),
		strings.Contains(line, "Failed to connect tunnel"),
		strings.Contains(line, "Tunnel connection failed"):
		masqueUp.Store(false)
	}
}

// logMark returns a position for lastLineSince.
func logMark() int {
	logs.mu.Lock()
	defer logs.mu.Unlock()
	return logs.total
}

// lastLineSince returns the newest line written after mark that contains
// substr, or "".
func lastLineSince(mark int, substr string) string {
	logs.mu.Lock()
	defer logs.mu.Unlock()
	newer := logs.total - mark
	for i := len(logs.lines) - 1; i >= 0 && i >= len(logs.lines)-newer; i-- {
		if strings.Contains(logs.lines[i], substr) {
			return logs.lines[i]
		}
	}
	return ""
}

// Logs returns the recent log lines, oldest first.
func Logs() string {
	logs.mu.Lock()
	defer logs.mu.Unlock()
	return strings.Join(logs.lines, "\n")
}

// EchoLogs also copies log output to stderr (desktop tooling).
func EchoLogs(on bool) { logs.echo.Store(on) }

var _ io.Writer = logs
