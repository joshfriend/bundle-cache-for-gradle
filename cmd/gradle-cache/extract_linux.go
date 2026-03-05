package main

import (
	"archive/tar"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/alecthomas/errors"
)

// extractTarPlatform uses sequential extraction on Linux. The Linux VFS
// writeback path coalesces dirty pages most efficiently when a single writer
// produces sequential writes, matching the behaviour of GNU tar. Parallel
// writes from multiple goroutines fragment the writeback queue and are
// consistently slower on Linux ext4/xfs despite identical throughput on APFS.
func extractTarPlatform(r io.Reader, dir string) error {
	return extractTarSeq(r, dir)
}

// extractTarSeq extracts a tar stream sequentially using pooled buffers.
// One goroutine reads and writes, avoiding goroutine-scheduling overhead and
// VFS writeback fragmentation — the same pattern GNU tar uses on Linux.
func extractTarSeq(r io.Reader, dir string) error {
	tr := tar.NewReader(r)
	cleanDir := filepath.Clean(dir) + string(os.PathSeparator)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return errors.Wrap(err, "read tar entry")
		}
		target := filepath.Join(dir, hdr.Name)
		if !strings.HasPrefix(filepath.Clean(target)+string(os.PathSeparator), cleanDir) {
			return errors.Errorf("tar entry %q escapes destination directory", hdr.Name)
		}
		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, hdr.FileInfo().Mode()); err != nil {
				return errors.Errorf("mkdir %s: %w", hdr.Name, err)
			}
		case tar.TypeReg:
			bufPtr := extractBufPool.Get().(*[]byte)
			if int64(cap(*bufPtr)) >= hdr.Size {
				*bufPtr = (*bufPtr)[:hdr.Size]
			} else {
				*bufPtr = make([]byte, hdr.Size)
			}
			if _, err := io.ReadFull(tr, *bufPtr); err != nil {
				extractBufPool.Put(bufPtr)
				return errors.Errorf("read %s: %w", hdr.Name, err)
			}
			if err := os.MkdirAll(filepath.Dir(target), 0o750); err != nil {
				extractBufPool.Put(bufPtr)
				return errors.Errorf("mkdir %s: %w", hdr.Name, err)
			}
			if err := os.WriteFile(target, *bufPtr, hdr.FileInfo().Mode()); err != nil {
				extractBufPool.Put(bufPtr)
				return errors.Errorf("write %s: %w", hdr.Name, err)
			}
			extractBufPool.Put(bufPtr)
		case tar.TypeSymlink:
			if err := os.MkdirAll(filepath.Dir(target), 0o750); err != nil {
				return errors.Errorf("mkdir for symlink %s: %w", hdr.Name, err)
			}
			if err := os.Symlink(hdr.Linkname, target); err != nil {
				return errors.Errorf("symlink %s → %s: %w", hdr.Name, hdr.Linkname, err)
			}
		}
	}
	return nil
}
