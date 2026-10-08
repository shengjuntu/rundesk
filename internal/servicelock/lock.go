// Package servicelock holds a kernel lock for the entire server lifetime.
package servicelock

import (
	"fmt"
	"os"
	"strings"
)

const marker = "rundesk-kernel-lock-v1\n"

type Lock struct{ file *os.File }

func Acquire(path string) (*Lock, error) {
	f, e := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if e != nil {
		return nil, e
	}
	fail := func(e error) (*Lock, error) { f.Close(); return nil, e }
	if e = lockFile(f); e != nil {
		return fail(fmt.Errorf("data directory is already locked or kernel locking is unavailable: %w", e))
	}
	b := make([]byte, 4096)
	n, e := f.ReadAt(b, 0)
	if e != nil && n == 0 {
		info, se := f.Stat()
		if se != nil || info.Size() != 0 {
			return fail(fmt.Errorf("cannot inspect lock file: %w", e))
		}
	}
	if n > 0 && !strings.HasPrefix(string(b[:n]), marker) {
		return fail(fmt.Errorf("legacy lock file exists: stop the old RunDesk process before removing this file once; never remove a live lock"))
	}
	if e = f.Truncate(0); e != nil {
		return fail(e)
	}
	if _, e = f.WriteAt([]byte(fmt.Sprintf("%spid=%d\n", marker, os.Getpid())), 0); e != nil {
		return fail(e)
	}
	return &Lock{file: f}, nil
}

// Leave the file in place: unlinking a locked inode permits a second owner.
func (l *Lock) Close() error { return l.file.Close() }
