//go:build aix || darwin || dragonfly || freebsd || illumos || linux || netbsd || openbsd || solaris

package bindingkeys

import (
	"errors"
	"fmt"
	"os"

	"golang.org/x/sys/unix"
)

func openBundleFileNoFollow(path string) (*os.File, error) {
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		if errors.Is(err, unix.ELOOP) {
			return nil, errors.New("binding key bundle cache must not be a symlink")
		}
		return nil, &os.PathError{Op: "open", Path: path, Err: err}
	}
	return os.NewFile(uintptr(fd), path), nil
}

func validateOpenedBundleFile(file *os.File) error {
	var stat unix.Stat_t
	if err := unix.Fstat(int(file.Fd()), &stat); err != nil {
		return err
	}
	if stat.Mode&unix.S_IFMT != unix.S_IFREG {
		return errors.New("binding key bundle cache must be a regular file")
	}
	if os.FileMode(stat.Mode).Perm() != 0o600 {
		return fmt.Errorf("binding key bundle cache mode is %04o, want 0600", os.FileMode(stat.Mode).Perm())
	}
	if stat.Uid != uint32(os.Geteuid()) {
		return fmt.Errorf("binding key bundle cache owner is uid %d, want %d", stat.Uid, os.Geteuid())
	}
	return nil
}
