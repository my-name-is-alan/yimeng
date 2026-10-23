//go:build !windows

package transcode

import "os"

func openTranscodeFile(path string) (*os.File, error) { return os.Open(path) }
