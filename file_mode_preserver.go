package filemodepreserver

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"sort"
	"strconv"
	"strings"
	"syscall"
)

// FileMode captures the POSIX permission bits and extended attributes of a path.
type FileMode struct {
	Perm    fs.FileMode
	Xattrs  map[string][]byte
}

// Capture reads the permission bits and extended attributes of path.
// It returns an error if path does not exist or cannot be read.
func Capture(path string) (FileMode, error) {
	info, err := os.Stat(path)
	if err != nil {
		return FileMode{}, err
	}
	fm := FileMode{
		Perm:   info.Mode().Perm(),
		Xattrs: map[string][]byte{},
	}
	names, err := listXattrs(path)
	if err != nil {
		// On filesystems that do not support xattrs, capture only permissions.
		if errors.Is(err, syscall.ENOTSUP) || errors.Is(err, syscall.EOPNOTSUPP) {
			return fm, nil
		}
		return FileMode{}, err
	}
	for _, name := range names {
		val, err := getXattr(path, name)
		if err != nil {
			// Skip attributes that vanish between listing and reading.
			if errors.Is(err, syscall.ENODATA) || errors.Is(err, syscall.ENOENT) {
				continue
			}
			return FileMode{}, err
		}
		fm.Xattrs[name] = val
	}
	return fm, nil
}

// Restore applies the captured permissions and extended attributes to path.
// Permissions are applied first, then extended attributes. Attributes that
// existed before but are not in fm are left untouched.
func (fm FileMode) Restore(path string) error {
	if err := os.Chmod(path, fm.Perm); err != nil {
		return err
	}
	// Sort for deterministic application order; xattr ordering is unspecified.
	names := make([]string, 0, len(fm.Xattrs))
	for name := range fm.Xattrs {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if err := setXattr(path, name, fm.Xattrs[name]); err != nil {
			return err
		}
	}
	return nil
}

// Equal reports whether two FileMode values capture the same permissions and
// extended attributes. It compares xattr values byte-for-byte.
func (fm FileMode) Equal(other FileMode) bool {
	if fm.Perm != other.Perm {
		return false
	}
	if len(fm.Xattrs) != len(other.Xattrs) {
		return false
	}
	for name, val := range fm.Xattrs {
		otherVal, ok := other.Xattrs[name]
		if !ok || !bytesEqual(val, otherVal) {
			return false
		}
	}
	return true
}

// String returns a stable, human-readable representation of the captured mode.
// Extended attribute values are rendered as hexadecimal to avoid ambiguity.
func (fm FileMode) String() string {
	var b strings.Builder
	fmt.Fprintf(&b, "perm=%s", fm.Perm)
	names := make([]string, 0, len(fm.Xattrs))
	for name := range fm.Xattrs {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		fmt.Fprintf(&b, " %s=%x", name, fm.Xattrs[name])
	}
	return b.String()
}

// CopyFile copies src to dst and restores the captured FileMode of src on dst.
// The destination is created with the source's permission bits. Existing
// extended attributes on dst are overwritten for keys present in src.
func CopyFile(src, dst string) error {
	fm, err := Capture(src)
	if err != nil {
		return err
	}
	if err := copyContents(src, dst); err != nil {
		return err
	}
	return fm.Restore(dst)
}

// copyContents copies the file contents of src to dst, truncating dst if it
// exists. It creates dst with mode 0o600 so that a partially written file is
// not readable by other users before Restore applies the final mode.
func copyContents(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

// bytesEqual compares two byte slices without importing bytes, keeping the
// dependency surface to the standard library packages already in use.
func bytesEqual(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// listXattrs returns the names of extended attributes on path. On Linux this
// uses the listxattr syscall. On platforms without xattr support it returns
// syscall.ENOTSUP so callers can degrade gracefully.
func listXattrs(path string) ([]string, error) {
	buf := make([]byte, 1024)
	n, err := syscall.Listxattr(path, buf)
	if err != nil {
		return nil, err
	}
	if n > len(buf) {
		// The buffer was too small; grow and retry once. A second failure is
		// returned rather than looping, to keep the function bounded.
		buf = make([]byte, n)
		n, err = syscall.Listxattr(path, buf)
		if err != nil {
			return nil, err
		}
	}
	return splitNullTerminated(buf[:n]), nil
}

// getXattr reads a single extended attribute value.
func getXattr(path, name string) ([]byte, error) {
	buf := make([]byte, 1024)
	n, err := syscall.Getxattr(path, name, buf)
	if err != nil {
		return nil, err
	}
	if n > len(buf) {
		buf = make([]byte, n)
		n, err = syscall.Getxattr(path, name, buf)
		if err != nil {
			return nil, err
		}
	}
	return buf[:n], nil
}

// setXattr writes a single extended attribute value.
func setXattr(path, name string, value []byte) error {
	return syscall.Setxattr(path, name, value, 0)
}

// splitNullTerminated splits a buffer of NUL-terminated strings. The final
// entry may or may not have a trailing NUL depending on the kernel.
func splitNullTerminated(buf []byte) []string {
	var out []string
	start := 0
	for i, b := range buf {
		if b == 0 {
			if i > start {
				out = append(out, string(buf[start:i]))
			}
			start = i + 1
		}
	}
	if start < len(buf) {
		out = append(out, string(buf[start:]))
	}
	return out
}

// parseMode is a small helper used in tests to build a fs.FileMode from an
// octal string such as "0644". It is exported indirectly through tests only.
func parseMode(s string) (fs.FileMode, error) {
	n, err := strconv.ParseUint(s, 8, 32)
	if err != nil {
		return 0, err
	}
	return fs.FileMode(n), nil
}
