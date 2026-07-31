//go:build unix

package certificate

import (
	"errors"
	"os"

	"golang.org/x/sys/unix"
)

func openDirectoryNoFollow(path string) (*os.File, error) {
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_DIRECTORY|unix.O_NOFOLLOW, 0)
	if err != nil {
		return nil, &os.PathError{Op: "open directory", Path: path, Err: err}
	}
	file := os.NewFile(uintptr(fd), path)
	if file == nil {
		_ = unix.Close(fd)
		return nil, errors.New("create directory identity handle")
	}
	return file, nil
}

func openFileNoFollow(path string) (*os.File, error) {
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return nil, &os.PathError{Op: "open file", Path: path, Err: err}
	}
	file := os.NewFile(uintptr(fd), path)
	if file == nil {
		_ = unix.Close(fd)
		return nil, errors.New("create private file handle")
	}
	return file, nil
}

func syncOpenedDirectory(directory *os.File) error {
	if directory == nil {
		return errors.New("directory handle is required")
	}
	return directory.Sync()
}

func sameFilesystemPath(left, right string) bool { return left == right }
