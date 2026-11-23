package filemodepreserver

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

// TestCaptureAndRestorePerm checks that permission bits survive a round trip.
func TestCaptureAndRestorePerm(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	dst := filepath.Join(dir, "dst")
	if err := os.WriteFile(src, []byte("hello"), 0o640); err != nil {
		t.Fatal(err)
	}
	fm, err := Capture(src)
	if err != nil {
		t.Fatal(err)
	}
	if fm.Perm != 0o640 {
		t.Fatalf("perm = %o, want 640", fm.Perm)
	}
	if err := os.WriteFile(dst, []byte(""), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := fm.Restore(dst); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(dst)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o640 {
		t.Fatalf("restored perm = %o, want 640", info.Mode().Perm())
	}
}

// TestCopyFilePreservesPerm checks that CopyFile applies the source mode.
func TestCopyFilePreservesPerm(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	dst := filepath.Join(dir, "dst")
	if err := os.WriteFile(src, []byte("payload"), 0o604); err != nil {
		t.Fatal(err)
	}
	if err := CopyFile(src, dst); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(dst)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "payload" {
		t.Fatalf("contents = %q, want payload", data)
	}
	info, err := os.Stat(dst)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o604 {
		t.Fatalf("perm = %o, want 604", info.Mode().Perm())
	}
}

// TestCopyFileOverwritesExisting checks that an existing destination is
// replaced, not appended to.
func TestCopyFileOverwritesExisting(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	dst := filepath.Join(dir, "dst")
	if err := os.WriteFile(src, []byte("new"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dst, []byte("old contents that are longer"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := CopyFile(src, dst); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(dst)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "new" {
		t.Fatalf("contents = %q, want new", data)
	}
}

// TestCaptureMissingFile checks that Capture reports a missing path.
func TestCaptureMissingFile(t *testing.T) {
	dir := t.TempDir()
	_, err := Capture(filepath.Join(dir, "nope"))
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

// TestEqual compares two captured modes.
func TestEqual(t *testing.T) {
	a := FileMode{Perm: 0o644, Xattrs: map[string][]byte{"user.a": []byte("1")}}
	b := FileMode{Perm: 0o644, Xattrs: map[string][]byte{"user.a": []byte("1")}}
	c := FileMode{Perm: 0o600, Xattrs: map[string][]byte{"user.a": []byte("1")}}
	d := FileMode{Perm: 0o644, Xattrs: map[string][]byte{"user.a": []byte("2")}}
	if !a.Equal(b) {
		t.Fatal("a should equal b")
	}
	if a.Equal(c) {
		t.Fatal("a should not equal c")
	}
	if a.Equal(d) {
		t.Fatal("a should not equal d")
	}
}

// TestString checks the stable rendering of a captured mode.
func TestString(t *testing.T) {
	fm := FileMode{Perm: 0o644, Xattrs: map[string][]byte{"user.b": []byte{0x01}, "user.a": []byte{0x02}}}
	got := fm.String()
	want := "perm=-rw-r--r-- user.a=02 user.b=01"
	if got != want {
		t.Fatalf("String() = %q, want %q", got, want)
	}
}

// TestSplitNullTerminated covers trailing and non-trailing NUL separators.
func TestSplitNullTerminated(t *testing.T) {
	cases := []struct {
		in   []byte
		want []string
	}{
		{[]byte("a\x00b\x00"), []string{"a", "b"}},
		{[]byte("a\x00b"), []string{"a", "b"}},
		{[]byte(""), nil},
		{[]byte("\x00"), nil},
	}
	for _, tc := range cases {
		got := splitNullTerminated(tc.in)
		if len(got) != len(tc.want) {
			t.Fatalf("split(%q) = %v, want %v", tc.in, got, tc.want)
		}
		for i := range got {
			if got[i] != tc.want[i] {
				t.Fatalf("split(%q)[%d] = %q, want %q", tc.in, i, got[i], tc.want[i])
			}
		}
	}
}

// TestXattrRoundTrip exercises extended attributes when the filesystem
// supports them. If the filesystem does not, the test is skipped rather than
// failing, because xattr support is a property of the platform, not the code.
func TestXattrRoundTrip(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	dst := filepath.Join(dir, "dst")
	if err := os.WriteFile(src, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	name := "user.filemodepreserver.test"
	value := []byte("value-1")
	if err := syscall.Setxattr(src, name, value, 0); err != nil {
		if err == syscall.ENOTSUP || err == syscall.EOPNOTSUPP {
			t.Skip("filesystem does not support extended attributes")
		}
		t.Fatal(err)
	}
	fm, err := Capture(src)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := fm.Xattrs[name]; !ok {
		t.Fatalf("captured xattrs = %v, missing %q", fm.Xattrs, name)
	}
	if err := os.WriteFile(dst, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := fm.Restore(dst); err != nil {
		t.Fatal(err)
	}
	got, err := getXattr(dst, name)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(value) {
		t.Fatalf("restored xattr = %q, want %q", got, value)
	}
}

// TestParseMode checks the octal parser used to construct test fixtures.
func TestParseMode(t *testing.T) {
	m, err := parseMode("0755")
	if err != nil {
		t.Fatal(err)
	}
	if m != 0o755 {
		t.Fatalf("parseMode = %o, want 755", m)
	}
	if _, err := parseMode("not-octal"); err == nil {
		t.Fatal("expected error for invalid octal")
	}
}
