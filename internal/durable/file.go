// Package durable provides single-process, atomically replaced JSON snapshots.
// It is a local demo store, not a substitute for a transactional database.
package durable

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

type File struct {
	path   string
	lock   *os.File
	failed error
	closed bool
}

// Open takes an advisory exclusive lock. All readers/writers must use this API.
func Open(path string, dst any) (*File, bool, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, false, err
	}
	lock, err := os.OpenFile(path+".lock", os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, false, err
	}
	if err = syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		lock.Close()
		return nil, false, fmt.Errorf("store already in use: %w", err)
	}
	f := &File{path: path, lock: lock}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return f, false, nil
	}
	if err == nil {
		err = json.Unmarshal(data, dst)
	}
	if err != nil {
		f.Close()
		return nil, false, fmt.Errorf("load state: %w", err)
	}
	return f, true, nil
}

// Save fsyncs the replacement and directory before acknowledging durability.
// On any failure this handle is poisoned; the caller must stop serving writes.
func (f *File) Save(value any) (err error) {
	if f.closed {
		return os.ErrClosed
	}
	if f.failed != nil {
		return f.failed
	}
	defer func() {
		if err != nil {
			f.failed = err
		}
	}()
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(f.path), ".snapshot-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer os.Remove(name)
	if _, err = tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err = tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err = tmp.Close(); err != nil {
		return err
	}
	if err = os.Rename(name, f.path); err != nil {
		return err
	}
	dir, err := os.Open(filepath.Dir(f.path))
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}
func (f *File) Err() error {
	if f.closed {
		return os.ErrClosed
	}
	return f.failed
}
func (f *File) Close() error {
	if f.closed {
		return nil
	}
	f.closed = true
	return f.lock.Close()
}
