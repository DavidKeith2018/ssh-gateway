//go:build !windows

package gateway

import "os"

func replaceSecretFile(from, to string) error {
	return os.Rename(from, to)
}
