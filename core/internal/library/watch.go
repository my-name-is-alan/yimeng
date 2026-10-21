package library

import (
	"context"
	"errors"
	"io/fs"
	"path/filepath"
	"strings"
	"time"

	"github.com/fsnotify/fsnotify"
)

type WatchOptions struct {
	Mode           string        // auto | fsnotify | poll | off
	Debounce       time.Duration // quiet period after the last fs event before scanning
	RescanInterval time.Duration // periodic incremental rescan for polled roots; 0 = never
}

// Watch keeps the library in sync until ctx is done. Roots are watched with
// fsnotify (debounced incremental scan of the affected root) and fall back to
// periodic rescans when watching is not possible: network mounts in "auto"
// mode, inotify watch limits (ENOSPC / EMFILE), or watcher errors.
func (s *Scanner) Watch(ctx context.Context, o WatchOptions) {
	if o.Mode == "off" || o.Mode == "" {
		return
	}
	if o.Debounce <= 0 {
		o.Debounce = 5 * time.Second
	}
	var polled []string
	var fw *fsnotify.Watcher
	watched := map[string]bool{} // root -> watched
	if o.Mode != "poll" {
		var err error
		fw, err = fsnotify.NewWatcher()
		if err != nil {
			s.log.Warn("fs watch unavailable; falling back to periodic rescan", "err", err)
			fw = nil
		}
	}
	for _, root := range s.opts.Roots {
		switch {
		case fw == nil:
			polled = append(polled, root)
		case o.Mode == "auto" && isNetworkFS(root):
			s.log.Info("network mount: using periodic rescan", "root", root)
			polled = append(polled, root)
		default:
			if err := addRecursive(fw, root); err != nil {
				s.log.Warn("fs watch failed (watch limit?); falling back to periodic rescan", "root", root, "err", err)
				removeRecursive(fw, root)
				polled = append(polled, root)
				continue
			}
			watched[root] = true
		}
	}
	if fw != nil {
		defer fw.Close()
	}
	var events <-chan fsnotify.Event
	var errs <-chan error
	if fw != nil && len(watched) > 0 {
		events, errs = fw.Events, fw.Errors
	}
	var tick <-chan time.Time
	if len(polled) > 0 && o.RescanInterval > 0 {
		t := time.NewTicker(o.RescanInterval)
		defer t.Stop()
		tick = t.C
	}
	if events == nil && tick == nil {
		return
	}

	dirty := map[string]bool{}
	timer := time.NewTimer(time.Hour)
	timer.Stop()
	arm := func() { timer.Reset(o.Debounce) }

	for {
		select {
		case <-ctx.Done():
			return
		case ev, ok := <-events:
			if !ok {
				events = nil
				continue
			}
			if ignoredPath(ev.Name) {
				continue
			}
			if ev.Has(fsnotify.Create) {
				if st, err := statDir(ev.Name); err == nil && st {
					if err := addRecursive(fw, ev.Name); err != nil {
						s.log.Warn("fs watch add failed", "dir", ev.Name, "err", err)
					}
				}
			}
			if r := s.rootOf(ev.Name); r != "" {
				dirty[r] = true
				arm()
			}
		case err, ok := <-errs:
			if !ok {
				errs = nil
				continue
			}
			s.log.Warn("fs watch error; rescanning watched roots", "err", err)
			for r := range watched {
				dirty[r] = true
			}
			arm()
		case <-timer.C:
			for r := range dirty {
				_, err := s.Start(ctx, StartRequest{Root: r})
				if errors.Is(err, ErrScanRunning) {
					arm() // try again after the running scan
					continue
				}
				if err != nil {
					s.log.Warn("watch scan", "root", r, "err", err)
				}
				delete(dirty, r)
			}
		case <-tick:
			for _, r := range polled {
				dirty[r] = true
			}
			arm()
		}
	}
}

func ignoredPath(p string) bool {
	for _, seg := range strings.Split(filepath.ToSlash(p), "/") {
		if skipDirName(seg) {
			return true
		}
	}
	return false
}

func skipDirName(name string) bool {
	return (strings.HasPrefix(name, ".") && name != "." && name != "..") ||
		name == "@eaDir" || name == "#recycle" || name == "@Recycle" || name == "#snapshot"
}

func addRecursive(w *fsnotify.Watcher, dir string) error {
	return filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			if p == dir {
				return err
			}
			return nil
		}
		if !d.IsDir() {
			return nil
		}
		if p != dir && skipDirName(d.Name()) {
			return filepath.SkipDir
		}
		return w.Add(p)
	})
}

func removeRecursive(w *fsnotify.Watcher, root string) {
	for _, p := range w.WatchList() {
		if p == root || strings.HasPrefix(p, root+string(filepath.Separator)) {
			_ = w.Remove(p)
		}
	}
}
