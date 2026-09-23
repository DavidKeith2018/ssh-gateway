package gateway

import (
	"golang.org/x/sys/windows"
	"os"
)

// 受保护的 DACL 只授予当前用户和 SYSTEM 权限。
func protectWindows(path string, directory bool) error {
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return err
	}
	inherit := ""
	if directory {
		inherit = "OICI"
	}
	sd, err := windows.SecurityDescriptorFromString("D:P(A;" + inherit + ";FA;;;" + user.User.Sid.String() + ")(A;" + inherit + ";FA;;;SY)")
	if err != nil {
		return err
	}
	acl, _, err := sd.DACL()
	if err != nil {
		return err
	}
	return windows.SetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION, nil, nil, acl, nil)
}
func protectFile(path string) error                     { return protectWindows(path, false) }
func protectDirectory(path string) error                { return protectWindows(path, true) }
func checkPrivateFile(path string, _ os.FileInfo) error { return protectFile(path) }
