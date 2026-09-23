package gateway

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

func DefaultDataDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	switch runtime.GOOS {
	case "windows":
		root := os.Getenv("LOCALAPPDATA")
		if root == "" {
			return "", fmt.Errorf("无法确定 LOCALAPPDATA")
		}
		return filepath.Join(root, "SSH Gateway"), nil
	case "darwin":
		return filepath.Join(home, "Library", "Application Support", "SSH Gateway"), nil
	default:
		root := os.Getenv("XDG_DATA_HOME")
		if root == "" {
			root = filepath.Join(home, ".local", "share")
		}
		return filepath.Join(root, "ssh-gateway"), nil
	}
}

// 服务持有文件锁；进程结束自动释放，不删除锁文件以免产生锁竞争。
func AcquireInstance(dir string) (*os.File, error) {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	if err := protectDirectory(dir); err != nil {
		return nil, err
	}
	path := filepath.Join(dir, "service.lock")
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	if err := protectFile(path); err != nil {
		file.Close()
		return nil, err
	}
	if err := lockFile(file); err != nil {
		file.Close()
		return nil, fmt.Errorf("此数据目录已有服务运行，请先退出该服务：%w", err)
	}
	return file, nil
}

// 数据使用者持有共享维护锁；离线恢复持有排他锁，防止命令行写入与恢复交错。
func acquireMaintenance(dir string, exclusive bool) (*os.File, error) {
	file, err := os.OpenFile(filepath.Join(dir, "maintenance.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	if err = protectFile(file.Name()); err != nil {
		file.Close()
		return nil, err
	}
	if exclusive {
		err = lockFile(file)
	} else {
		err = sharedLockFile(file)
	}
	if err != nil {
		file.Close()
		return nil, fmt.Errorf("数据目录正在使用或恢复中，请停止相关服务和命令后重试：%w", err)
	}
	return file, nil
}
