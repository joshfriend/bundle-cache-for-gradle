package gradlecache

import (
	"archive/tar"
	"io"
	"os"
	"path/filepath"
	"sync"
	"syscall"

	"github.com/alecthomas/errors"
)

var extractBufPool = sync.Pool{
	New: func() interface{} {
		b := make([]byte, 0, 256<<10)
		return &b
	},
}

const mmapThreshold = 64 * 1024

var darwinExtractWorkers = 8

func extractTarPlatform(r io.Reader, dir string) error {
	return extractTarGoRouted(r, func(name string) string {
		return filepath.Join(dir, name)
	}, false)
}

func extractTarPlatformRouted(r io.Reader, targetFn func(string) string, skipExisting bool) error {
	return extractTarGoRouted(r, targetFn, skipExisting)
}

func extractTarGoRouted(r io.Reader, targetFn func(string) string, skipExisting bool) error {
	type fileJob struct {
		path string
		mode os.FileMode
		buf  *[]byte
	}

	numWorkers := darwinExtractWorkers
	jobs := make(chan fileJob, numWorkers*2)

	var workerErrs []error
	var mu sync.Mutex
	var wg sync.WaitGroup

	for range numWorkers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for job := range jobs {
				if err := writeFileMacos(job.path, *job.buf, job.mode); err != nil {
					mu.Lock()
					workerErrs = append(workerErrs, err)
					mu.Unlock()
				}
				extractBufPool.Put(job.buf)
			}
		}()
	}

	createdDirs := make(map[string]struct{})
	ensureDir := func(d string, mode os.FileMode) error {
		if _, ok := createdDirs[d]; ok {
			return nil
		}
		if err := os.MkdirAll(d, mode); err != nil { //nolint:gosec
			return err
		}
		createdDirs[d] = struct{}{}
		return nil
	}

	tr := tar.NewReader(r)
	var readErr error
readLoop:
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			readErr = errors.Wrap(err, "read tar entry")
			break
		}

		name, err := safeTarEntryName(hdr.Name)
		if err != nil {
			readErr = err
			break
		}

		target := targetFn(name)

		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := ensureDir(target, hdr.FileInfo().Mode()); err != nil {
				readErr = errors.Errorf("mkdir %s: %w", hdr.Name, err)
				break readLoop
			}

		case tar.TypeReg:
			if skipExisting {
				if _, err := os.Lstat(target); err == nil {
					continue
				}
			}
			if err := ensureDir(filepath.Dir(target), 0o755); err != nil {
				readErr = errors.Errorf("mkdir %s: %w", hdr.Name, err)
				break readLoop
			}
			bufPtr := extractBufPool.Get().(*[]byte)
			if int64(cap(*bufPtr)) >= hdr.Size {
				*bufPtr = (*bufPtr)[:hdr.Size]
			} else {
				*bufPtr = make([]byte, hdr.Size)
			}
			if _, err := io.ReadFull(tr, *bufPtr); err != nil {
				extractBufPool.Put(bufPtr)
				readErr = errors.Errorf("read %s: %w", hdr.Name, err)
				break readLoop
			}
			jobs <- fileJob{path: target, mode: hdr.FileInfo().Mode(), buf: bufPtr}

		case tar.TypeSymlink:
			if skipExisting {
				if _, err := os.Lstat(target); err == nil {
					continue
				}
			}
			if err := safeSymlinkTarget(name, hdr.Linkname); err != nil {
				readErr = err
				break readLoop
			}
			if err := ensureDir(filepath.Dir(target), 0o755); err != nil {
				readErr = errors.Errorf("mkdir for symlink %s: %w", hdr.Name, err)
				break readLoop
			}
			if err := os.Symlink(hdr.Linkname, target); err != nil {
				readErr = errors.Errorf("symlink %s → %s: %w", hdr.Name, hdr.Linkname, err)
				break readLoop
			}

		case tar.TypeLink:
			if skipExisting {
				if _, err := os.Lstat(target); err == nil {
					continue
				}
			}
			linkName, err := safeTarEntryName(hdr.Linkname)
			if err != nil {
				readErr = errors.Errorf("unsafe hardlink target %q: %w", hdr.Linkname, err)
				break readLoop
			}
			linkTarget := targetFn(linkName)
			if err := ensureDir(filepath.Dir(target), 0o755); err != nil {
				readErr = errors.Errorf("mkdir for hardlink %s: %w", hdr.Name, err)
				break readLoop
			}
			if err := os.Link(linkTarget, target); err != nil {
				readErr = errors.Errorf("hardlink %s → %s: %w", hdr.Name, hdr.Linkname, err)
				break readLoop
			}
		}
	}

	close(jobs)
	wg.Wait()

	var allErrs []error
	if readErr != nil {
		allErrs = append(allErrs, readErr)
	}
	allErrs = append(allErrs, workerErrs...)
	return errors.Join(allErrs...)
}

func writeFileMacos(path string, data []byte, mode os.FileMode) error {
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE|os.O_TRUNC, mode)
	if err != nil {
		return errors.Errorf("open %s: %w", path, err)
	}
	defer f.Close() //nolint:errcheck

	if len(data) >= mmapThreshold {
		if tErr := f.Truncate(int64(len(data))); tErr == nil {
			if mapped, mErr := syscall.Mmap(int(f.Fd()), 0, len(data), syscall.PROT_WRITE, syscall.MAP_SHARED); mErr == nil {
				copy(mapped, data)
				if uErr := syscall.Munmap(mapped); uErr != nil {
					return errors.Errorf("munmap %s: %w", path, uErr)
				}
				return nil
			}
		}
	}

	if _, err := f.Write(data); err != nil {
		return errors.Errorf("write %s: %w", path, err)
	}
	return nil
}
