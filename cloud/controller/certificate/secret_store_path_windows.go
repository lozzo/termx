//go:build windows

package certificate

import (
	"errors"
	"os"
	"strings"

	"golang.org/x/sys/windows"
)

func openDirectoryNoFollow(path string) (*os.File, error) {
	return openWindowsPathNoFollow(path, true)
}

func openFileNoFollow(path string) (*os.File, error) {
	return openWindowsPathNoFollow(path, false)
}

func openWindowsPathNoFollow(path string, directory bool) (*os.File, error) {
	pointer, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return nil, err
	}
	flags := uint32(windows.FILE_ATTRIBUTE_NORMAL | windows.FILE_FLAG_OPEN_REPARSE_POINT)
	if directory {
		flags |= windows.FILE_FLAG_BACKUP_SEMANTICS
	}
	access := uint32(windows.GENERIC_READ | windows.READ_CONTROL | windows.FILE_READ_ATTRIBUTES)
	if directory {
		access = windows.READ_CONTROL | windows.FILE_READ_ATTRIBUTES
	}
	handle, err := windows.CreateFile(
		pointer,
		access,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
		nil,
		windows.OPEN_EXISTING,
		flags,
		0,
	)
	if err != nil {
		return nil, &os.PathError{Op: "open no-follow", Path: path, Err: err}
	}
	var information windows.ByHandleFileInformation
	if err := windows.GetFileInformationByHandle(handle, &information); err != nil {
		_ = windows.CloseHandle(handle)
		return nil, err
	}
	if information.FileAttributes&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 || (information.FileAttributes&windows.FILE_ATTRIBUTE_DIRECTORY != 0) != directory {
		_ = windows.CloseHandle(handle)
		return nil, errors.New("certificate store path component has an unsafe type")
	}
	file := os.NewFile(uintptr(handle), path)
	if file == nil {
		_ = windows.CloseHandle(handle)
		return nil, errors.New("create certificate store identity handle")
	}
	return file, nil
}

func syncOpenedDirectory(*os.File) error { return nil }

func sameFilesystemPath(left, right string) bool { return strings.EqualFold(left, right) }
