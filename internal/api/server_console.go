package api

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/doeshing/nekoclaw/internal/logger"
)

const (
	defaultConsoleTailLines = 400
	maxConsoleTailLines     = 2000
	consolePingInterval     = 15 * time.Second
)

type consoleSnapshotEvent struct {
	Type    string   `json:"type"`
	LogPath string   `json:"log_path"`
	Lines   []string `json:"lines"`
}

type consoleAppendEvent struct {
	Type string `json:"type"`
	Line string `json:"line"`
}

type consolePingEvent struct {
	Type string `json:"type"`
}

func (s *Server) handleConsoleStream(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		respondError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		respondError(w, http.StatusInternalServerError, "streaming not supported")
		return
	}

	logPath := strings.TrimSpace(s.consoleLogPath)
	if logPath == "" {
		respondErrorDetail(
			w,
			http.StatusServiceUnavailable,
			"console_unavailable",
			"console log is not available",
		)
		return
	}

	tailLines, err := parseConsoleTailLines(r.URL.Query().Get("tail"))
	if err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}

	lines, err := tailFileLines(logPath, tailLines)
	if err != nil {
		respondErrorDetail(
			w,
			http.StatusServiceUnavailable,
			"console_unavailable",
			"console log is not available",
		)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	if err := writeSSEEvent(w, "snapshot", consoleSnapshotEvent{
		Type:    "snapshot",
		LogPath: logPath,
		Lines:   lines,
	}); err != nil {
		return
	}
	flusher.Flush()

	lineCh, unsubscribe := logger.SubscribeLines()
	defer unsubscribe()

	pingTicker := time.NewTicker(consolePingInterval)
	defer pingTicker.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case line := <-lineCh:
			if strings.TrimSpace(line) == "" {
				continue
			}
			if err := writeSSEEvent(w, "append", consoleAppendEvent{
				Type: "append",
				Line: line,
			}); err != nil {
				return
			}
			flusher.Flush()
		case <-pingTicker.C:
			if err := writeSSEEvent(w, "ping", consolePingEvent{Type: "ping"}); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}

func parseConsoleTailLines(raw string) (int, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return defaultConsoleTailLines, nil
	}

	parsed, err := strconv.Atoi(value)
	if err != nil {
		return 0, fmt.Errorf("tail must be an integer")
	}
	if parsed < 0 {
		return 0, fmt.Errorf("tail must be >= 0")
	}
	if parsed > maxConsoleTailLines {
		return maxConsoleTailLines, nil
	}
	return parsed, nil
}

func tailFileLines(path string, limit int) ([]string, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	if limit <= 0 {
		return []string{}, nil
	}

	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	ring := make([]string, 0, limit)
	start := 0
	for scanner.Scan() {
		line := scanner.Text()
		if len(ring) < limit {
			ring = append(ring, line)
			continue
		}
		ring[start] = line
		start = (start + 1) % limit
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	if len(ring) < limit {
		return ring, nil
	}

	lines := make([]string, 0, len(ring))
	lines = append(lines, ring[start:]...)
	lines = append(lines, ring[:start]...)
	return lines, nil
}

func writeSSEEvent(w io.Writer, event string, payload any) error {
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, data); err != nil {
		return err
	}
	return nil
}
