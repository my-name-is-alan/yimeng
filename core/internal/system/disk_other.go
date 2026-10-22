//go:build !(linux || darwin || freebsd)

package system

func freeBytes(string) (int64, bool) { return 0, false }
func inotifyWatches() int            { return 0 }
