//go:build !linux && !darwin && !windows

package servicelock

import (
	"fmt"
	"os"
)

func lockFile(f *os.File) error {
	return fmt.Errorf("kernel data-directory locking is unsupported on this platform")
}
