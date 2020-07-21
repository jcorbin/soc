package socutil

import (
	"os"
	"path/filepath"
)

// FindWDFile attempts to find a named file relative to the current working
// directory, checking every parent directory until one is found.
// It returns stat info and an absolute path or an error.
func FindWDFile(name string) (os.FileInfo, string, error) {
	info, err := os.Stat(name)
	if err == nil {
		path, err := filepath.Abs(name)
		return info, path, err
	}

	wd, err := os.Getwd()
	if err != nil {
		return nil, "", err
	}

	// TODO should we apply a limit to how far up we'll go?
	for ; len(wd) > 0; wd = filepath.Dir(wd) {
		path := filepath.Join(wd, name)
		if info, err = os.Stat(path); err == nil {
			path, err = filepath.Abs(path)
			return info, path, err
		}
	}

	return nil, "", nil
}
