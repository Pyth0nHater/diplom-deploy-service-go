package service

import (
	"bytes"
	"regexp"
	"strings"
)

const eventWriterHistoryLimit = 12

var ansiPattern = regexp.MustCompile(`\x1b\[[0-9;]*[A-Za-z]`)

type eventWriter struct {
	stage  string
	result *DeployResult
	emit   EventFn
	buffer bytes.Buffer
	recent []string
}

func newEventWriter(stage string, result *DeployResult, emit EventFn) *eventWriter {
	return &eventWriter{
		stage:  stage,
		result: result,
		emit:   emit,
	}
}

func (w *eventWriter) Write(p []byte) (int, error) {
	for _, b := range p {
		if b == '\n' || b == '\r' {
			if err := w.flushBuffer(); err != nil {
				return 0, err
			}
			continue
		}
		if err := w.buffer.WriteByte(b); err != nil {
			return 0, err
		}
	}
	return len(p), nil
}

func (w *eventWriter) Flush() error {
	return w.flushBuffer()
}

func (w *eventWriter) Summary() string {
	if len(w.recent) == 0 {
		return ""
	}
	return strings.Join(w.recent, " | ")
}

func (w *eventWriter) flushBuffer() error {
	message := strings.TrimSpace(sanitizeLogLine(w.buffer.String()))
	w.buffer.Reset()
	if message == "" {
		return nil
	}
	w.remember(message)
	return w.emit(EventLevelInfo, w.stage, message, w.result)
}

func (w *eventWriter) remember(message string) {
	if message == "" {
		return
	}
	w.recent = append(w.recent, message)
	if len(w.recent) > eventWriterHistoryLimit {
		w.recent = w.recent[len(w.recent)-eventWriterHistoryLimit:]
	}
}

func sanitizeLogLine(line string) string {
	line = ansiPattern.ReplaceAllString(line, "")
	line = strings.ReplaceAll(line, "\t", " ")
	return strings.TrimSpace(line)
}
