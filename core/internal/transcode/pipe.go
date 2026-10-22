package transcode

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os/exec"
	"strconv"
	"strings"
)

// pipeArgs is args() for stdin → stdout. Inputs are untrusted remote bytes,
// so demuxing is restricted to audio formats and protocol use is disabled.
func (p Profile) pipeArgs() []string {
	sp := specs[p.Format]
	a := []string{"-nostdin", "-hide_banner", "-loglevel", "error",
		"-protocol_whitelist", "pipe", "-i", "pipe:0", "-map", "0:a:0", "-vn", "-sn", "-dn", "-c:a", sp.encoder}
	if p.BitrateKbps > 0 {
		a = append(a, "-b:a", strconv.Itoa(p.BitrateKbps)+"k")
	}
	if p.Format == "mp3" {
		a = append(a, "-id3v2_version", "3")
	}
	return append(a, "-f", sp.muxer, "pipe:1")
}

type pipeReader struct {
	io.ReadCloser
	cmd    *exec.Cmd
	cancel context.CancelFunc
	stderr *bytes.Buffer
	t      *Transcoder
	done   bool
}

func (r *pipeReader) Read(b []byte) (int, error) {
	n, err := r.ReadCloser.Read(b)
	if err == io.EOF && !r.done {
		r.done = true
		if werr := r.cmd.Wait(); werr != nil {
			msg := strings.TrimSpace(r.stderr.String())
			if len(msg) > 300 {
				msg = msg[:300]
			}
			return n, fmt.Errorf("transcode: %v: %s", werr, msg)
		}
	}
	return n, err
}

func (r *pipeReader) Close() error {
	r.cancel()
	r.ReadCloser.Close()
	if !r.done {
		r.done = true
		r.cmd.Wait()
	}
	<-r.t.sem
	return nil
}

// Pipe transcodes a stream (e.g. a proxied online track) on the fly without
// caching it: online audio is never written to disk. The caller closes in
// when done; the returned reader yields encoded bytes and must be closed.
func (t *Transcoder) Pipe(ctx context.Context, in io.Reader, p Profile) (io.ReadCloser, error) {
	if !t.Supports(p) || p.IsOriginal() {
		return nil, ErrUnavailable
	}
	select {
	case t.sem <- struct{}{}:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	cctx, cancel := context.WithCancel(ctx)
	cmd := exec.CommandContext(cctx, t.ffmpeg, p.pipeArgs()...)
	cmd.Stdin = in
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.StdoutPipe()
	if err == nil {
		err = cmd.Start()
	}
	if err != nil {
		cancel()
		<-t.sem
		return nil, err
	}
	return &pipeReader{ReadCloser: out, cmd: cmd, cancel: cancel, stderr: &stderr, t: t}, nil
}
