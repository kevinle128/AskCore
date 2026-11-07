package bootstrap

import (
	"errors"
	"io"
	"os"
)

// MaxFileBytes is the largest source file that is read.
const MaxFileBytes = 1 << 20

var (
	errNotRegular = errors.New("not a regular file")
	errTooLarge   = errors.New("file is over the size limit")
)

// readRegular follows symbolic links and reads a regular file. It never reads
// more than MaxFileBytes+1 bytes. size is the size reported by stat. A missing
// file returns an error that satisfies errors.Is(err, fs.ErrNotExist).
func readRegular(path string) (data []byte, size int64, err error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, 0, err
	}
	if !info.Mode().IsRegular() {
		return nil, 0, errNotRegular
	}
	size = info.Size()
	f, err := os.Open(path)
	if err != nil {
		return nil, size, err
	}
	defer func() { _ = f.Close() }()
	data, err = io.ReadAll(io.LimitReader(f, MaxFileBytes+1))
	if err != nil {
		return nil, size, err
	}
	if len(data) > MaxFileBytes {
		return nil, size, errTooLarge
	}
	return data, size, nil
}
