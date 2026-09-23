//go:build !windows

package gateway

import (
	"fmt"
	"os"
)

func protectFile(path string) error      { return os.Chmod(path, 0600) }
func protectDirectory(path string) error { return os.Chmod(path, 0700) }
func checkPrivateFile(path string, info os.FileInfo) error {
	if info.Mode().Perm()&0077 != 0 {
		return fmt.Errorf("私密文件权限必须为 0600：%s", path)
	}
	return nil
}
