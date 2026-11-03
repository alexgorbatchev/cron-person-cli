package cronrc

import (
	"os"
	"path/filepath"
	"testing"
)

// realAncestorCronrc returns a .cronrc the machine has above dir, outside any
// fixture, which discovery would legitimately find.
func realAncestorCronrc(dir string) string {
	for curr := filepath.Dir(dir); ; curr = filepath.Dir(curr) {
		candidate := filepath.Join(curr, ".cronrc")
		if info, err := os.Stat(candidate); err == nil && info.Mode().IsRegular() {
			return candidate
		}
		if filepath.Dir(curr) == curr {
			return ""
		}
	}
}

// A fixture .cronrc above the start bounds the walk, so the result never
// depends on files outside the fixture.
func TestFindCronrcStopsAtTheNearestFixtureAncestor(t *testing.T) {
	root := t.TempDir()
	nested := filepath.Join(root, "a", "b", "c")
	if err := os.MkdirAll(nested, 0o700); err != nil {
		t.Fatal(err)
	}
	boundary := filepath.Join(root, ".cronrc")
	nearest := filepath.Join(root, "a", ".cronrc")
	for _, path := range []string{boundary, nearest} {
		if err := os.WriteFile(path, []byte("@daily true\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if found, err := FindCronrc(nested); err != nil || found != nearest {
		t.Fatalf("FindCronrc = %q, %v; want %q", found, err, nearest)
	}
	if err := os.Remove(nearest); err != nil {
		t.Fatal(err)
	}
	if found, err := FindCronrc(nested); err != nil || found != boundary {
		t.Fatalf("FindCronrc = %q, %v; want the fixture boundary %q", found, err, boundary)
	}
}

// Finding nothing depends on every directory up to the root, which no
// fixture controls, so the case runs only where none of them has a .cronrc.
func TestFindCronrcReportsNoneWithoutAncestors(t *testing.T) {
	dir := t.TempDir()
	if real := realAncestorCronrc(dir); real != "" {
		t.Skipf("%s exists above the test directory, so discovery would rightly find it", real)
	}
	if found, err := FindCronrc(dir); err != nil || found != "" {
		t.Fatalf("FindCronrc = %q, %v; want none", found, err)
	}
}
