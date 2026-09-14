// SPDX-License-Identifier: MIT
// Copyright (C) 2026 Petr Stupka

// Package fswatch is the one file watcher behind every hot-reload: the
// config, the prompt and starter files, pricing.yaml. It only signals —
// whoever owns the file re-reads it — and it watches the file's
// directory rather than the file, because editors save by writing a
// temp file and renaming it over the original, which ends a watch on
// the file itself.
package fswatch

import (
	"context"
	"path/filepath"
	"time"

	"github.com/fsnotify/fsnotify"
)

// Watch signals on ch whenever the file at path changes on disk. It
// watches the directory (editors rename-replace files, which would kill
// a file watch), filters by name, and debounces bursts. Signals are
// dropped rather than blocking, like the capture's events.
func Watch(ctx context.Context, path string, ch chan<- struct{}) error {
	w, err := fsnotify.NewWatcher()
	if err != nil {
		return err
	}
	dir := filepath.Dir(path)
	if err := w.Add(dir); err != nil {
		w.Close()
		return err
	}

	go func() {
		defer w.Close()
		// Trailing debounce: bursts (editors save via temp + rename, our
		// own atomic writes do too) coalesce into one signal ~300ms after
		// the last event, so the final change is never lost.
		var timer *time.Timer
		fire := make(chan struct{}, 1)
		for {
			select {
			case <-ctx.Done():
				return
			case ev, ok := <-w.Events:
				if !ok {
					return
				}
				if filepath.Clean(ev.Name) != filepath.Clean(path) {
					continue
				}
				if timer == nil {
					timer = time.AfterFunc(300*time.Millisecond, func() {
						select {
						case fire <- struct{}{}:
						default:
						}
					})
				} else {
					timer.Reset(300 * time.Millisecond)
				}
			case <-fire:
				select {
				case ch <- struct{}{}:
				default: // drop rather than block
				}
			case _, ok := <-w.Errors:
				if !ok {
					return
				}
			}
		}
	}()
	return nil
}
