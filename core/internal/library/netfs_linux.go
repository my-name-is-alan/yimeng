//go:build linux

package library

import "golang.org/x/sys/unix"

// isNetworkFS reports whether path lives on a filesystem where inotify does
// not see remote changes (NFS, SMB/CIFS, FUSE, 9p).
func isNetworkFS(path string) bool {
	var st unix.Statfs_t
	if err := unix.Statfs(path, &st); err != nil {
		return false
	}
	switch uint32(st.Type) {
	case 0x6969, // NFS
		0x517B,     // SMB
		0xFF534D42, // CIFS
		0xFE534D42, // SMB2
		0x65735546, // FUSE (rclone, sshfs, ...)
		0x01021997: // 9p (Docker Desktop / WSL mounts)
		return true
	}
	return false
}
