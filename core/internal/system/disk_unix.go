//go:build linux || darwin || freebsd

package system

import (
	"os"
	"strconv"
	"strings"

	"golang.org/x/sys/unix"
)

func freeBytes(path string) (int64, bool) {
	var st unix.Statfs_t
	if err := unix.Statfs(path, &st); err != nil {
		return 0, false
	}
	return int64(st.Bavail) * int64(st.Bsize), true
}

// inotifyWatches returns fs.inotify.max_user_watches on Linux (0 elsewhere).
func inotifyWatches() int {
	b, err := os.ReadFile("/proc/sys/fs/inotify/max_user_watches")
	if err != nil {
		return 0
	}
	n, _ := strconv.Atoi(strings.TrimSpace(string(b)))
	return n
}
