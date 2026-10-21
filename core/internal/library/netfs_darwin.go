//go:build darwin

package library

import "golang.org/x/sys/unix"

func isNetworkFS(path string) bool {
	var st unix.Statfs_t
	if err := unix.Statfs(path, &st); err != nil {
		return false
	}
	n := make([]byte, 0, len(st.Fstypename))
	for _, c := range st.Fstypename {
		if c == 0 {
			break
		}
		n = append(n, byte(c))
	}
	switch string(n) {
	case "smbfs", "nfs", "afpfs", "webdav", "macfuse", "osxfuse":
		return true
	}
	return false
}
