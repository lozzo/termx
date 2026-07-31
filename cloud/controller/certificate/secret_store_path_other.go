//go:build !unix && !windows

package certificate

import (
	"errors"
	"os"
)

func syncOpenedDirectory(*os.File) error {
	return errors.New("certificate store directory sync is unsupported")
}
