package flightrecorder

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// cleanupSnapshots prunes snapshot files in r.snapshotDir beyond r.maxSnapshots,
// keeping the newest. It must be called with r.mu held. Errors are reported via
// the [LoggerHook] and never propagated: a retention failure must not break a
// successful snapshot.
func (r *Recorder) cleanupSnapshots() {
	entries, err := os.ReadDir(r.snapshotDir)
	if err != nil {
		r.loggerHook("flightrecorder: retention scan failed for %s: %v", r.snapshotDir, err)

		return
	}

	type snapshotFile struct {
		path    string
		modTime int64
	}

	var files []snapshotFile

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		name := entry.Name()
		if !strings.HasPrefix(name, r.snapshotPrefix) {
			continue
		}

		if !strings.HasSuffix(name, traceSuffix) && !strings.HasSuffix(name, traceGzSuffix) {
			continue
		}

		info, err := entry.Info()
		if err != nil {
			continue
		}

		files = append(files, snapshotFile{
			path:    filepath.Join(r.snapshotDir, name),
			modTime: info.ModTime().UnixNano(),
		})
	}

	if len(files) <= r.maxSnapshots {
		return
	}

	// Newest first, so the tail is what gets removed.
	sort.Slice(files, func(i, j int) bool {
		return files[i].modTime > files[j].modTime
	})

	removed := 0

	for _, f := range files[r.maxSnapshots:] {
		if err := os.Remove(f.path); err != nil {
			r.loggerHook("flightrecorder: retention could not remove %s: %v", f.path, err)

			continue
		}

		removed++
	}

	if removed > 0 {
		r.loggerHook("flightrecorder: retention removed %d old snapshot(s) in %s", removed, r.snapshotDir)
	}
}
