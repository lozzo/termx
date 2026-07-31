//go:build unix

package certificate

import (
	"errors"
	"os"
)

func syncOpenedDirectory(directory *os.File) error {
	if directory == nil {
		return errors.New("directory handle is required")
	}
	return directory.Sync()
}
