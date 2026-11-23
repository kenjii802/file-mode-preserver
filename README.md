# file-mode-preserver

Capture, store, and restore POSIX file permissions and extended attributes across file copy operations.

```go
package main

import (
	"fmt"

	filemodepreserver "filemodepreserver"
)

func main() {
	fm, err := filemodepreserver.Capture("/etc/hosts")
	if err != nil {
		panic(err)
	}
	fmt.Println(fm.Perm) // e.g. -rw-r--r--

	if err := fm.Restore("/tmp/hosts.copy"); err != nil {
		panic(err)
	}

	if err := filemodepreserver.CopyFile("/etc/hosts", "/tmp/hosts.copy"); err != nil {
		panic(err)
	}
}
```

## Why this exists

`io.Copy` and `os.WriteFile` do not carry permission bits or extended attributes
from a source file to a destination. On Linux, tools like `cp -p` or `install -m`
fill that gap at the shell level, but Go programs that copy files in-process have
no standard-library way to preserve the metadata that matters for security and
correctness: the setuid/setgid bits, restrictive modes, and labels such as
`security.selinux` or `user.*` annotations.

This library keeps the surface small and the behaviour predictable. It captures
the permission bits and extended attributes into a plain `FileMode` value that
the caller can store, compare, or pass around. `Restore` applies them. `CopyFile`
is a convenience that does both around a content copy.

The trade-off is scope: only permission bits and extended attributes are
captured. Ownership (uid/gid), timestamps, ACLs, and file flags are deliberately
out of scope, because they either require privileges the caller may not have or
need richer representations than a `FileMode` value should carry. If you need
those, do them explicitly at the call site.

## The awkward edge

Extended attributes are a filesystem feature, not a POSIX guarantee. On a
filesystem that does not support them, `Capture` returns a `FileMode` with an
empty `Xattrs` map rather than an error, and `Restore` simply applies the
permission bits. Callers that need to know whether xattrs were actually
preserved should check `len(fm.Xattrs)`. The test suite skips the xattr round
trip when the filesystem reports `ENOTSUP`, so it passes on tmpfs and ext4 alike.

`CopyFile` creates the destination with mode `0o600` before applying the
source mode. This avoids a window in which a partially written file is readable
by other users when the source mode is more permissive. The final mode is
applied by `Restore` after the content copy completes.

## Exported API

- `type FileMode struct { Perm fs.FileMode; Xattrs map[string][]byte }`
- `func Capture(path string) (FileMode, error)`
- `func (fm FileMode) Restore(path string) error`
- `func (fm FileMode) Equal(other FileMode) bool`
- `func (fm FileMode) String() string`
- `func CopyFile(src, dst string) error`
