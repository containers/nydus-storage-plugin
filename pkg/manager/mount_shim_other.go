//go:build !linux

package manager

import "errors"

const (
	// Linux mount flags used for tests; values chosen to match linux for assertions.
	msBind    = 0x1000
	msRec     = 0x4000
	msRemount = 0x20
	msRdonly  = 0x1
)

type mountFunc func(source, target, fstype string, flags uintptr, data string) error

type unmountFunc func(target string, flags int) error

// On non-linux, return explicit errors by default so production runs fail fast.
// Unit tests replace these vars to stub platform-specific behavior.
var unixMount mountFunc = func(_, _, _ string, _ uintptr, _ string) error {
	return errors.New("mount is not supported on non-linux platforms")
}
var unixUnmount unmountFunc = func(_ string, _ int) error {
	return errors.New("unmount is not supported on non-linux platforms")
}
