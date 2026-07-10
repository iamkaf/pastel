package ui

import (
	"fmt"
	"path"
)

// SyncReport receives human-friendly sync progress events.
type SyncReport struct {
	Verbose    bool
	QuietOK    bool // if true (default for non-verbose), skip per-file "already good" lines
	pruned     []string
	wouldPrune []string
}

// NewSyncReport builds a reporter. Non-verbose mode only shows changes.
func NewSyncReport(verbose bool) *SyncReport {
	return &SyncReport{Verbose: verbose, QuietOK: !verbose}
}

func (r *SyncReport) Unchanged(filePath string) {
	if r == nil || r.QuietOK {
		return
	}
	Info(fmt.Sprintf("already good  %s", Dim(shortName(filePath))))
}

func (r *SyncReport) Download(filePath string) {
	if r == nil {
		return
	}
	OK(fmt.Sprintf("downloaded    %s", Blue(shortName(filePath))))
}

func (r *SyncReport) WouldDownload(filePath string) {
	if r == nil {
		return
	}
	Step(fmt.Sprintf("would download  %s", Blue(shortName(filePath))))
}

func (r *SyncReport) WouldUpdate(filePath string) {
	if r == nil {
		return
	}
	Step(fmt.Sprintf("would update    %s", Blue(shortName(filePath))))
}

func (r *SyncReport) Prune(filePath string) {
	if r == nil {
		return
	}
	r.pruned = append(r.pruned, filePath)
}

func (r *SyncReport) WouldPrune(filePath string) {
	if r == nil {
		return
	}
	r.wouldPrune = append(r.wouldPrune, filePath)
}

// Flush prints buffered prune lines (compressed when many).
func (r *SyncReport) Flush() {
	if r == nil {
		return
	}
	printPruneList(r.pruned, false)
	printPruneList(r.wouldPrune, true)
	r.pruned = nil
	r.wouldPrune = nil
}

func printPruneList(list []string, dry bool) {
	if len(list) == 0 {
		return
	}
	const maxLines = 5
	if len(list) > maxLines {
		if dry {
			Step(fmt.Sprintf("would remove %d extra files", len(list)))
		} else {
			Warn(fmt.Sprintf("removed %d extra mods/files", len(list)))
		}
		Detail(shortName(list[0]) + ", " + shortName(list[1]) + ", …")
		return
	}
	for _, p := range list {
		if dry {
			Step(fmt.Sprintf("would remove    %s", shortName(p)))
		} else {
			Warn(fmt.Sprintf("removed extra  %s", shortName(p)))
		}
	}
}

func shortName(p string) string {
	return path.Base(p)
}
