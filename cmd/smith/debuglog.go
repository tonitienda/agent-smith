package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/tonitienda/agent-smith/internal/provider"
)

const debugLogFile = "debug.log"

// debugLog is a per-session append-only text log for interactive debugging.
type debugLog struct {
	mu   sync.Mutex
	path string
	file *os.File
}

func openDebugLog(dir string) (*debugLog, error) {
	path := filepath.Join(dir, debugLogFile)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return nil, fmt.Errorf("open debug log %s: %w", path, err)
	}
	return &debugLog{path: path, file: f}, nil
}

func (l *debugLog) Path() string {
	if l == nil {
		return ""
	}
	return l.path
}

func (l *debugLog) Close() error {
	if l == nil || l.file == nil {
		return nil
	}
	return l.file.Close()
}

func (l *debugLog) Printf(format string, args ...any) {
	if l == nil || l.file == nil {
		return
	}
	line := time.Now().UTC().Format(time.RFC3339Nano) + " " + fmt.Sprintf(format, args...) + "\n"
	l.mu.Lock()
	defer l.mu.Unlock()
	_, _ = l.file.WriteString(line)
}

func wrapProvidersWithDebugLog(providers map[string]provider.Provider, log *debugLog) map[string]provider.Provider {
	if log == nil {
		return providers
	}
	out := make(map[string]provider.Provider, len(providers))
	for name, p := range providers {
		out[name] = loggedProvider{inner: p, log: log}
	}
	return out
}

type loggedProvider struct {
	inner provider.Provider
	log   *debugLog
}

func (p loggedProvider) Name() string { return p.inner.Name() }

func (p loggedProvider) Stream(ctx context.Context, req provider.Request) (provider.Stream, error) {
	start := time.Now()
	p.log.Printf("provider stream start vendor=%s model=%q context_blocks=%d tools=%d",
		p.inner.Name(), req.Model, len(req.Context), len(req.Tools))

	s, err := p.inner.Stream(ctx, req)
	if err != nil {
		logProviderError(p.log, "provider stream open failed", p.inner.Name(), req.Model, time.Since(start), err)
		return nil, err
	}
	return &loggedStream{
		Stream:  s,
		log:     p.log,
		vendor:  p.inner.Name(),
		model:   req.Model,
		started: start,
	}, nil
}

type loggedStream struct {
	provider.Stream

	log     *debugLog
	vendor  string
	model   string
	started time.Time
	once    sync.Once
}

func (s *loggedStream) Err() error {
	err := s.Stream.Err()
	s.report(err)
	return err
}

func (s *loggedStream) Close() error {
	err := s.Stream.Close()
	s.report(s.Stream.Err())
	return err
}

func (s *loggedStream) report(err error) {
	s.once.Do(func() {
		if err != nil {
			logProviderError(s.log, "provider stream ended with error", s.vendor, s.model, time.Since(s.started), err)
			return
		}
		s.log.Printf("provider stream complete vendor=%s model=%q duration=%s", s.vendor, s.model, time.Since(s.started).Round(time.Millisecond))
	})
}

func logProviderError(log *debugLog, prefix, vendor, model string, dur time.Duration, err error) {
	if log == nil || err == nil {
		return
	}
	if pe, ok := provider.AsError(err); ok {
		log.Printf("%s vendor=%s model=%q duration=%s kind=%s retryable=%t status=%d message=%q cause=%v",
			prefix, vendor, model, dur.Round(time.Millisecond), pe.Kind, pe.Retryable, pe.StatusCode, pe.Message, pe.Err)
		return
	}
	log.Printf("%s vendor=%s model=%q duration=%s error=%v", prefix, vendor, model, dur.Round(time.Millisecond), err)
}
