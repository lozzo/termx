//go:build windows

// Package securefs 统一私有状态文件的平台权限边界。
package securefs

import (
	"errors"
	"fmt"
	"os"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	accessAllowedCompoundACEType       = 0x4
	accessAllowedObjectACEType         = 0x5
	accessAllowedCallbackACEType       = 0x9
	accessAllowedCallbackObjectACEType = 0xb
)

// SecureDirectory 为 Windows 私有目录写入受保护且可继承的 DACL。
// 子项只允许当前用户、SYSTEM 和本机 Administrators，不能继承宽松的父目录写权限。
func SecureDirectory(path string) error { return secureWindowsPath(path, true) }

// SecureFile 为 Windows 私钥、credential 或 runtime record 写入受保护 DACL。
// 权限失败时调用方必须停止使用该秘密，不能回退到 chmod 的只读位映射。
func SecureFile(path string) error { return secureWindowsPath(path, false) }

// IsPrivateFile verifies the opened file rather than querying security metadata by path.
func IsPrivateFile(path string, info os.FileInfo) bool {
	if info == nil || !info.Mode().IsRegular() {
		return false
	}
	file, err := os.Open(path)
	if err != nil {
		return false
	}
	defer file.Close()
	openedInfo, err := file.Stat()
	return err == nil && os.SameFile(info, openedInfo) && ValidatePrivateFileHandle(file) == nil
}

// ValidatePrivateFileHandle validates owner and protected DACL on the supplied handle.
// Only current-user, SYSTEM, and Administrators allow ACEs are accepted.
func ValidatePrivateFileHandle(file *os.File) error {
	if file == nil {
		return errors.New("private file handle is required")
	}
	info, err := file.Stat()
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return errors.New("private file handle must reference a regular file")
	}
	descriptor, err := windows.GetSecurityInfo(
		windows.Handle(file.Fd()),
		windows.SE_FILE_OBJECT,
		windows.OWNER_SECURITY_INFORMATION|windows.DACL_SECURITY_INFORMATION,
	)
	if err != nil {
		return err
	}
	return validatePrivateWindowsDescriptor(descriptor)
}

func validatePrivateWindowsDescriptor(descriptor *windows.SECURITY_DESCRIPTOR) error {
	current, system, administrators, err := privateWindowsSIDs()
	if err != nil {
		return err
	}
	if descriptor == nil || !descriptor.IsValid() {
		return errors.New("private file security descriptor is invalid")
	}
	owner, _, err := descriptor.Owner()
	if err != nil || owner == nil {
		return errors.New("private file owner is unavailable")
	}
	control, _, err := descriptor.Control()
	if err != nil {
		return err
	}
	dacl, _, err := descriptor.DACL()
	if err != nil || dacl == nil {
		return errors.New("private file DACL is unavailable")
	}
	rules := make([]privateAccessRule, 0, dacl.AceCount)
	for index := uint32(0); index < uint32(dacl.AceCount); index++ {
		var ace *windows.ACCESS_ALLOWED_ACE
		if err := windows.GetAce(dacl, index, &ace); err != nil || ace == nil {
			return errors.New("read private file DACL ACE")
		}
		switch ace.Header.AceType {
		case windows.ACCESS_ALLOWED_ACE_TYPE:
			sid := (*windows.SID)(unsafe.Pointer(&ace.SidStart))
			if !sid.IsValid() {
				return errors.New("private file DACL contains an invalid SID")
			}
			rules = append(rules, privateAccessRule{allow: true, trustee: sid.String()})
		case accessAllowedCompoundACEType, accessAllowedObjectACEType, accessAllowedCallbackACEType, accessAllowedCallbackObjectACEType:
			return errors.New("private file DACL contains an unsupported allow ACE")
		default:
			rules = append(rules, privateAccessRule{allow: false})
		}
	}
	if !privateDescriptorAllowed(owner.String(), current.String(), system.String(), administrators.String(), control&windows.SE_DACL_PROTECTED != 0, rules) {
		return errors.New("private file owner or protected DACL is unsafe")
	}
	return nil
}

func secureWindowsPath(path string, directory bool) error {
	current, _, _, err := privateWindowsSIDs()
	if err != nil {
		return err
	}
	inheritance := ""
	if directory {
		inheritance = "OICI"
	}
	sddl := fmt.Sprintf(
		"D:P(A;%s;FA;;;%s)(A;%s;FA;;;SY)(A;%s;FA;;;BA)",
		inheritance,
		current.String(),
		inheritance,
		inheritance,
	)
	descriptor, err := windows.SecurityDescriptorFromString(sddl)
	if err != nil {
		return err
	}
	dacl, _, err := descriptor.DACL()
	if err != nil {
		return err
	}
	return windows.SetNamedSecurityInfo(
		path,
		windows.SE_FILE_OBJECT,
		windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION,
		nil,
		nil,
		dacl,
		nil,
	)
}

func privateWindowsSIDs() (*windows.SID, *windows.SID, *windows.SID, error) {
	currentUser, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil || currentUser == nil || currentUser.User.Sid == nil {
		return nil, nil, nil, fmt.Errorf("resolve current Windows user SID: %w", err)
	}
	system, err := windows.CreateWellKnownSid(windows.WinLocalSystemSid)
	if err != nil {
		return nil, nil, nil, err
	}
	administrators, err := windows.CreateWellKnownSid(windows.WinBuiltinAdministratorsSid)
	if err != nil {
		return nil, nil, nil, err
	}
	return currentUser.User.Sid, system, administrators, nil
}
