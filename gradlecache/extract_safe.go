package gradlecache

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/alecthomas/errors"
)

func safeTarEntryName(name string) (string, error) {
	clean := filepath.Clean(name)
	if filepath.IsAbs(clean) {
		return "", errors.Errorf("tar entry %q: absolute path not allowed", name)
	}
	if clean == ".." || strings.HasPrefix(clean, ".."+string(os.PathSeparator)) {
		return "", errors.Errorf("tar entry %q escapes destination directory", name)
	}
	return clean, nil
}

func safeSymlinkTarget(entryName, linkname string) error {
	if filepath.IsAbs(linkname) {
		return errors.Errorf("symlink %q -> %q: absolute target not allowed", entryName, linkname)
	}
	resolved := filepath.Clean(filepath.Join(filepath.Dir(entryName), linkname))
	if resolved == ".." || strings.HasPrefix(resolved, ".."+string(os.PathSeparator)) {
		return errors.Errorf("symlink %q -> %q escapes destination directory", entryName, linkname)
	}
	return nil
}
