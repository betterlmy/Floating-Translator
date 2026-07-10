//go:build !windows

package config

import (
	"os"
)

func replaceFile(source string, target string) error {
	return os.Rename(source, target)
}
