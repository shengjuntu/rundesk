// Package containerruntime supplies the trusted, read-only container helper.
package containerruntime

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"
)

func Dispatch(args []string) (bool, error) {
	if len(args) == 0 {
		return false, nil
	}
	switch args[0] {
	case "__container_idle":
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer stop()
		<-ctx.Done()
		return true, nil
	case "__container_probe":
		if _, e := exec.LookPath("codex"); e != nil {
			return true, fmt.Errorf("application image must provide codex on PATH: %w", e)
		}
		return true, prepareProcess(&exec.Cmd{})
	case "__container_seed":
		if len(args) != 2 {
			return true, errors.New("seed requires CODEX_HOME")
		}
		return true, Seed("/opt/rundesk-seed", args[1])
	case "__container_exec":
		if len(args) < 3 {
			return true, errors.New("exec requires lease and argv")
		}
		return true, Supervise(args[1], args[2:])
	}
	return false, nil
}

// Images may seed config.toml and complete skill directories. Never copy auth,
// native history, or links; existing project configuration is never overwritten.
func Seed(src, dst string) error {
	if err := os.MkdirAll(dst, 0700); err != nil {
		return err
	}
	root, err := os.OpenRoot(dst)
	if err != nil {
		return err
	}
	defer root.Close()
	if _, err = root.Stat(".rundesk-seeded"); err == nil {
		return nil
	} else if !os.IsNotExist(err) {
		return err
	}
	for _, name := range []string{"config.toml", "skills"} {
		from := filepath.Join(src, name)
		if _, err = os.Lstat(from); os.IsNotExist(err) {
			continue
		} else if err != nil {
			return err
		}
		err = filepath.WalkDir(from, func(path string, d fs.DirEntry, e error) error {
			if e != nil {
				return e
			}
			if d.Type()&os.ModeSymlink != 0 {
				return fmt.Errorf("seed symlink unsupported: %s", path)
			}
			rel, e := filepath.Rel(src, path)
			if e != nil {
				return e
			}
			if d.IsDir() {
				return root.MkdirAll(rel, 0700)
			}
			info, e := d.Info()
			if e != nil {
				return e
			}
			if !info.Mode().IsRegular() {
				return fmt.Errorf("unsupported seed file: %s", path)
			}
			mode := os.FileMode(0600)
			if info.Mode()&0111 != 0 {
				mode = 0700
			}
			out, e := root.OpenFile(rel, os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode)
			if os.IsExist(e) {
				return nil
			}
			if e != nil {
				return e
			}
			in, e := os.Open(path)
			if e != nil {
				out.Close()
				return e
			}
			defer in.Close()
			_, e = io.Copy(out, in)
			ce := out.Close()
			if e != nil {
				_ = root.Remove(rel)
				return e
			}
			return ce
		})
		if err != nil {
			return err
		}
	}
	return root.WriteFile(".rundesk-seeded", []byte("1\n"), 0600)
}

// A host-refreshed lease survives neither host failure nor connection closure.
// It also revokes a child when the docker-exec client disconnects without EOF.
func Supervise(lease string, args []string) error {
	valid := func() bool {
		v, e := os.Stat(lease)
		return e == nil && v.Mode().IsRegular() && time.Since(v.ModTime()) < 20*time.Second
	}
	if !valid() {
		return errors.New("RunDesk execution lease missing or expired")
	}
	cmd := exec.Command(args[0], args[1:]...)
	if e := prepareProcess(cmd); e != nil {
		return e
	}
	in, e := cmd.StdinPipe()
	if e != nil {
		return e
	}
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	if e = cmd.Start(); e != nil {
		in.Close()
		return e
	}
	defer killChildren()
	eof := make(chan struct{})
	go func() { _, _ = io.Copy(in, os.Stdin); _ = in.Close(); close(eof) }()
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	tick := time.NewTicker(250 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case e = <-done:
			return e
		case <-eof:
			killChildren()
			return <-done
		case <-ctx.Done():
			killChildren()
			return <-done
		case <-tick.C:
			if !valid() {
				killChildren()
				<-done
				return errors.New("RunDesk execution lease ended; children stopped")
			}
		}
	}
}
