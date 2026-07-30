//go:build windows

package bindingkeys

import (
	"errors"
	"os"

	"golang.org/x/sys/windows"
)

func openBundleFileNoFollow(path string) (*os.File, error) {
	pathPointer, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return nil, err
	}
	handle, err := windows.CreateFile(
		pathPointer,
		windows.GENERIC_READ|windows.READ_CONTROL,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
		nil,
		windows.OPEN_EXISTING,
		windows.FILE_ATTRIBUTE_NORMAL|windows.FILE_FLAG_OPEN_REPARSE_POINT,
		0,
	)
	if err != nil {
		return nil, &os.PathError{Op: "open", Path: path, Err: err}
	}
	return os.NewFile(uintptr(handle), path), nil
}

func validateOpenedBundleFile(file *os.File) error {
	handle := windows.Handle(file.Fd())
	var information windows.ByHandleFileInformation
	if err := windows.GetFileInformationByHandle(handle, &information); err != nil {
		return err
	}
	if information.FileAttributes&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 {
		return errors.New("binding key bundle cache must not be a symlink or reparse point")
	}
	if information.FileAttributes&windows.FILE_ATTRIBUTE_DIRECTORY != 0 {
		return errors.New("binding key bundle cache must be a regular file")
	}
	descriptor, err := windows.GetSecurityInfo(handle, windows.SE_FILE_OBJECT, windows.OWNER_SECURITY_INFORMATION)
	if err != nil {
		return err
	}
	if descriptor == nil {
		return errors.New("binding key bundle cache has no security descriptor")
	}
	owner, _, err := descriptor.Owner()
	if err != nil {
		return err
	}
	currentUser, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return err
	}
	if owner == nil || !owner.Equals(currentUser.User.Sid) {
		return errors.New("binding key bundle cache must be owned by the current user")
	}
	return nil
}
