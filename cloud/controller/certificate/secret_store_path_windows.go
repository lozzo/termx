//go:build windows

package certificate

import "os"

func syncOpenedDirectory(*os.File) error { return nil }
