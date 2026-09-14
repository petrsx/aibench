// SPDX-License-Identifier: MIT
// Copyright (C) 2026 Petr Stupka

// Package applog wires the process's slog default to the app log file —
// ~/.aibench/logs/aibench.log, one fixed user-level location so `aibench
// logs` finds it from anywhere. Log paths, profile names, and statuses;
// never credentials or env values (the capture layer already redacts, the
// log must too).
//
// Named applog, not log: it is the *application's* diagnostic log — what
// the app did (start, profile switch, watcher failure, crash) — which is
// neither the stdlib log nor the session's record of messages.
package applog

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/petrsx/aibench/internal/config"
)

// maxSize is the size guard: above it the file rotates to .old on Init —
// one previous generation, no rotation daemon.
const maxSize = 5 << 20

// Path is the log file's location: `aibench.log` beside the config file
// in force, next to the settings.json the app writes there too. The log
// is about a session, and which endpoints a session talks to is the
// config's business — so a project with its own aibench.yaml keeps its
// own log, and the home config keeps the one in ~/.aibench.
func Path() string {
	return filepath.Join(filepath.Dir(config.FilePath()), "aibench.log")
}

// current is the open log file. Init holds it here so a re-Init can
// close it first: Windows cannot rename (rotate) a file the process
// still has open.
var current *os.File

// Init points slog's default at the app log file (debug flips the level)
// and returns the path. Failures fall back to a discarding logger — the
// app must run fine without a log.
func Init(enabled, debug bool) (string, error) {
	// Off is off: slog goes nowhere and no file is opened or rotated, so
	// a default install leaves nothing behind. --debug overrides, so a
	// problem can be captured without first turning logging on and
	// reproducing it.
	if !enabled && !debug {
		Close()
		slog.SetDefault(slog.New(slog.DiscardHandler))
		return "", nil
	}
	path := Path()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", err
	}
	Close()
	if info, err := os.Stat(path); err == nil && info.Size() > maxSize {
		_ = os.Rename(path, path+".old")
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return "", fmt.Errorf("open log: %w", err)
	}
	current = f
	level := slog.LevelInfo
	if debug {
		level = slog.LevelDebug
	}
	slog.SetDefault(slog.New(slog.NewTextHandler(f, &slog.HandlerOptions{Level: level})))
	return path, nil
}

// Close releases the log file handle (tests need it on Windows, where an
// open file can be neither renamed nor deleted; the app itself just lets
// process exit reclaim it).
func Close() {
	if current != nil {
		_ = current.Close()
		current = nil
	}
}
