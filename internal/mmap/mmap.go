package mmap

import (
	"fmt"
	"os"
	"syscall"

	"golang.org/x/sys/unix"
)

type File struct {
	Data []byte
	orig []byte
}

func Open(path string) (*File, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return nil, err
	}
	size := st.Size()
	if int64(int(size+4095)) != size+4095 {
		return nil, fmt.Errorf("%s: too large for mmap", path)
	}
	n := int(size)
	if n == 0 {
		return &File{}, nil
	}
	data, err := unix.Mmap(int(f.Fd()), 0, (n+4095)&^4095, syscall.PROT_READ, syscall.MAP_SHARED)
	if err != nil {
		return nil, fmt.Errorf("mmap %s: %v", path, err)
	}
	return &File{
		Data: data[:n],
		orig: data,
	}, nil
}

func (f *File) Close() error {
	return unix.Munmap(f.orig)
}
