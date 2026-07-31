//go:build !unix && !windows

package certificate

import (
	"errors"
	"os"
)

func openDirectoryNoFollow(string) (*os.File, error) {
	return nil, errors.New("certificate store directory identity is unsupported")
}

func openFileNoFollow(string) (*os.File, error) {
	return nil, errors.New("certificate store file identity is unsupported")
}

func syncOpenedDirectory(*os.File) error {
	return errors.New("certificate store directory sync is unsupported")
}

func sameFilesystemPath(left, right string) bool { return left == right }
