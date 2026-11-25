package displayreference

import (
	"encoding/base64"
	"os"
	"os/exec"
	"runtime"
	"testing"

	"github.com/alexgorbatchev/cron-person-cli/internal/display"
)

func TestReferenceBoundaries(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix byte cases; Windows native fixtures are separate")
	}
	encode := func(value string) string { return base64.StdEncoding.EncodeToString([]byte(value)) }
	for _, tc := range []struct {
		value, home   string
		present       bool
		path, message string
	}{
		{"/home/test/file", "/home/test", true, "~/file", "~/file"},
		{"prefix/home/test/file", "/home/test", true, "prefix/home/test/file", "prefix/home/test/file"},
		{"'/home/test/file'", "/home/test", true, "'/home/test/file'", "'~/file'"},
		{"/home/tested", "/home/test", true, "/home/tested", "/home/tested"},
		{"/x", "", false, "/x", "/x"},
		{"/x", "/", true, "~/x", "/x"},
		{"/a/\xff", "/a", true, "~/\xff", "~/\xff"},
	} {
		got, err := PathBytes(encode(tc.value), encode(tc.home), tc.present)
		if err != nil || got != encode(tc.path) {
			t.Fatalf("PathBytes(%q): %q %v", tc.value, got, err)
		}
		got, err = ErrorBytes(encode(tc.value), encode(tc.home), tc.present)
		if err != nil || got != encode(tc.message) {
			t.Fatalf("ErrorBytes(%q): %q %v", tc.value, got, err)
		}
	}
	for _, fn := range []func(string, string, bool) (string, error){PathBytes, ErrorBytes} {
		for _, args := range [][2]string{{"!", ""}, {"", "!"}, {"", encode("\x00")}} {
			if _, err := fn(args[0], args[1], true); err == nil {
				t.Fatalf("accepted invalid input: %#v", args)
			}
		}
	}
	t.Setenv("HOME", "/home/test")
	if FormatPath("/home/test/file") != "~/file" || FormatErrorText("open /home/test/file") != "open ~/file" {
		t.Fatal("production string wrappers did not fold HOME")
	}
	if err := os.Unsetenv("HOME"); err != nil {
		t.Fatal(err)
	}
	if _, err := PathBytes("", "", false); err != nil {
		t.Fatal(err)
	}
}

func TestMissingHomeSubprocess(t *testing.T) {
	if os.Getenv("DISPLAY_REFERENCE_CHILD") == "1" {
		key := "HOME"
		if runtime.GOOS == "windows" {
			key = "USERPROFILE"
		}
		if err := os.Unsetenv(key); err != nil {
			t.Fatal(err)
		}
		if FormatPath("/a") != "/a" || FormatErrorText("open /a") != "open /a" {
			t.Fatal("missing home changed the diagnostic")
		}
		return
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestMissingHomeSubprocess$")
	cmd.Env = append(os.Environ(), "DISPLAY_REFERENCE_CHILD=1")
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("child: %v\n%s", err, output)
	}
}

func TestNilErrorPanics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("nil error did not panic")
		}
	}()
	display.Error(nil)
}
