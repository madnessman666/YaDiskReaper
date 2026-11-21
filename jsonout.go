package main

import (
	"encoding/json"
	"log"
	"os"
	"sync"
	"sync/atomic"
	"time"
)

// jsonMode switches the CLI into a machine-readable event-stream mode used by
// the desktop GUI. Events are newline-delimited JSON objects written to stdout;
// human-readable logs keep going to stderr.
//
// Event catalogue (all objects have an "event" field):
//
//	starting   {}
//	info       {name, type}
//	scanning   {path}
//	scan_progress {files, bytes}
//	scan_done  {total, bytes}
//	progress   {done, total, skipped, failed, bytes_done, bytes_total,
//	            speed, active, waiting, activity}
//	file_done  {path, size, skipped}
//	file_failed{path, error}
//	finished   {ok, total, skipped, failed, bytes}
//	fatal      {message}
var jsonMode bool

var emitMu sync.Mutex

func emit(event string, fields map[string]any) {
	if !jsonMode {
		return
	}

	payload := make(map[string]any, len(fields)+1)
	payload["event"] = event
	for k, v := range fields {
		payload[k] = v
	}

	line, err := json.Marshal(payload)
	if err != nil {
		return
	}
	line = append(line, '\n')

	emitMu.Lock()
	_, _ = os.Stdout.Write(line)
	emitMu.Unlock()
}

// fatalEvent reports a terminal error in both modes and exits with status 1.
func fatalEvent(message string) {
	emit("fatal", map[string]any{"message": message})
	log.Printf("❌ %s", message)
	os.Exit(1)
}

func emitProgress(stats AriaStats) {
	if !jsonMode {
		return
	}

	bytesTotal := atomic.LoadInt64(&totalBytes)
	bytesDone := atomic.LoadInt64(&bytesFinished) + stats.ActiveBytes
	if bytesDone > bytesTotal {
		bytesDone = bytesTotal
	}

	emit("progress", map[string]any{
		"done":        atomic.LoadInt64(&doneFiles),
		"total":       atomic.LoadInt64(&totalFileCount),
		"skipped":     atomic.LoadInt64(&skippedCount),
		"failed":      atomic.LoadInt64(&failedCount),
		"bytes_done":  bytesDone,
		"bytes_total": bytesTotal,
		"speed":       stats.SpeedBytes,
		"active":      stats.Active,
		"waiting":     stats.Waiting,
		"activity":    getActivity(),
	})
}

// countScanned is called by the tree walker for every discovered file.
func countScanned(size int64) {
	atomic.AddInt64(&scanFiles, 1)
	atomic.AddInt64(&scanBytesFound, size)
}

// startScanReporter emits throttled scan progress until the returned stop
// function is called. Without it a large folder looks frozen in the GUI.
func startScanReporter() func() {
	if !jsonMode {
		return func() {}
	}

	done := make(chan struct{})
	go func() {
		ticker := time.NewTicker(500 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-ticker.C:
				emit("scan_progress", map[string]any{
					"files": atomic.LoadInt64(&scanFiles),
					"bytes": atomic.LoadInt64(&scanBytesFound),
				})
			}
		}
	}()

	var once sync.Once
	return func() { once.Do(func() { close(done) }) }
}
