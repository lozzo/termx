//go:build windows

// Package securefs 统一私有状态文件的平台权限边界。
package securefs

import (
	"fmt"
	"os"
	"unsafe"

	"golang.org/x/sys/windows"
)

// SecureDirectory 为 Windows 私有目录写入受保护且可继承的 DACL。
// 子项只允许当前用户、SYSTEM 和本机 Administrators，不能继承宽松的父目录写权限。
func SecureDirectory(path string) error { return secureWindowsPath(path, true) }

// SecureFile 为 Windows 私钥、credential 或 runtime record 写入受保护 DACL。
// 权限失败时调用方必须停止使用该秘密，不能回退到 chmod 的只读位映射。
func SecureFile(path string) error { return secureWindowsPath(path, false) }

// IsPrivateFile 验证 Windows 文件 owner 与受保护 DACL。
// 任何额外 allow ACE 都会失败，避免共享临时目录的继承权限扩大秘密读取范围。
func IsPrivateFile(path string, info os.FileInfo) bool {
	if info == nil || !info.Mode().IsRegular() {
		return false
	}
	current, system, administrators, err := privateWindowsSIDs()
	if err != nil {
		return false
	}
	descriptor, err := windows.GetNamedSecurityInfo(
		path,
		windows.SE_FILE_OBJECT,
		windows.OWNER_SECURITY_INFORMATION|windows.DACL_SECURITY_INFORMATION,
	)
	if err != nil || descriptor == nil || !descriptor.IsValid() {
		return false
	}
	owner, _, err := descriptor.Owner()
	if err != nil || owner == nil || !owner.Equals(current) {
		return false
	}
	control, _, err := descriptor.Control()
	if err != nil || control&windows.SE_DACL_PROTECTED == 0 {
		return false
	}
	dacl, _, err := descriptor.DACL()
	if err != nil || dacl == nil {
		return false
	}
	currentAllowed := false
	for index := uint32(0); index < uint32(dacl.AceCount); index++ {
		var ace *windows.ACCESS_ALLOWED_ACE
		if err := windows.GetAce(dacl, index, &ace); err != nil || ace == nil {
			return false
		}
		if ace.Header.AceType != windows.ACCESS_ALLOWED_ACE_TYPE {
			continue
		}
		sid := (*windows.SID)(unsafe.Pointer(&ace.SidStart))
		switch {
		case sid.Equals(current):
			currentAllowed = true
		case sid.Equals(system), sid.Equals(administrators):
		default:
			return false
		}
	}
	return currentAllowed
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
