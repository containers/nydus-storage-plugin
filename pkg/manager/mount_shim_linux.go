//go:build linux

package manager

import "golang.org/x/sys/unix"

const (
	msBind    = unix.MS_BIND
	msRec     = unix.MS_REC
	msRemount = unix.MS_REMOUNT
	msRdonly  = unix.MS_RDONLY
)

type mountFunc func(source, target, fstype string, flags uintptr, data string) error

type unmountFunc func(target string, flags int) error

var unixMount mountFunc = func(source, target, fstype string, flags uintptr, data string) error {
	return unix.Mount(source, target, fstype, flags, data)
}

var unixUnmount unmountFunc = func(target string, flags int) error {
	return unix.Unmount(target, flags)
}
