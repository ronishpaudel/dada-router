package logger

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/ronishpaudel/dada-router/internal/config"
	"github.com/ronishpaudel/dada-router/internal/redact"
)

// LogEntry represents a single JSON structured log line
type LogEntry struct {
	Timestamp time.Time `json:"timestamp"`
	Provider  string    `json:"provider,omitempty"`
	Model     string    `json:"model,omitempty"`
	Route     string    `json:"route,omitempty"`
	Status    string    `json:"status"` // "ok", "error", "panic"
	Tokens    int       `json:"tokens,omitempty"`
	Error     string    `json:"error,omitempty"`
	Prompt    string    `json:"prompt,omitempty"`   // only if LogPrompts == true
	Response  string    `json:"response,omitempty"` // only if LogPrompts == true
}

// Logger handles thread-safe structured logging with secret redaction
type Logger struct {
	mu         sync.Mutex
	enabled    bool
	logPrompts bool
	filePath   string
}

// NewLogger creates a Logger configured from Settings
func NewLogger(settings *config.Settings) (*Logger, error) {
	enabled := false
	logPrompts := false
	if settings != nil {
		enabled = settings.Log.Enabled
		logPrompts = settings.Log.LogPrompts
	}

	logPath, err := config.GetLogPath()
	if err != nil {
		return nil, err
	}

	return &Logger{
		enabled:    enabled,
		logPrompts: logPrompts,
		filePath:   logPath,
	}, nil
}

// LogEvent records metadata and optional prompt/response after an LLM call
func (l *Logger) LogEvent(providerName string, model string, route string, status string, tokens int, prompt string, response string, callErr error) {
	if !l.enabled {
		return
	}

	entry := LogEntry{
		Timestamp: time.Now().UTC(),
		Provider:  providerName,
		Model:     model,
		Route:     route,
		Status:    status,
		Tokens:    tokens,
	}

	if callErr != nil {
		entry.Error = redact.Redact(callErr.Error())
	}

	if l.logPrompts {
		entry.Prompt = redact.Redact(prompt)
		entry.Response = redact.Redact(response)
	}

	l.writeEntry(entry)
}

// LogPanic logs a recovered panic and scrubbed stack trace
func (l *Logger) LogPanic(rec interface{}, stack []byte) {
	if !l.enabled {
		return
	}

	scrubbedStack := redact.Redact(string(stack))
	entry := LogEntry{
		Timestamp: time.Now().UTC(),
		Status:    "panic",
		Error:     fmt.Sprintf("PANIC: %v\nStack:\n%s", rec, scrubbedStack),
	}

	l.writeEntry(entry)
}

func (l *Logger) writeEntry(entry LogEntry) {
	l.mu.Lock()
	defer l.mu.Unlock()

	data, err := json.Marshal(entry)
	if err != nil {
		return
	}
	data = append(data, '\n')

	dir := filepath.Dir(l.filePath)
	_ = os.MkdirAll(dir, 0700)

	f, err := os.OpenFile(l.filePath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		return
	}
	defer f.Close()

	_, _ = f.Write(data)
}

// ReadLogs reads and returns the contents of the log file, fully redacted
func (l *Logger) ReadLogs() (string, error) {
	l.mu.Lock()
	defer l.mu.Unlock()

	data, err := os.ReadFile(l.filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return "No logs found.", nil
		}
		return "", fmt.Errorf("failed to read log file: %w", err)
	}

	return redact.Redact(string(data)), nil
}

// Reset clears the log file
func (l *Logger) Reset() error {
	l.mu.Lock()
	defer l.mu.Unlock()

	if err := os.Remove(l.filePath); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}
