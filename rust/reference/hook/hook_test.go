package hookreference

import (
	"encoding/hex"
	"strings"
	"testing"
)

func TestRealGenerator(t *testing.T) {
	for _, shell := range []string{"bash", "zsh", "fish"} {
		for _, binary := range []string{"", "/opt/custom tool", "\x00\xff\"$HOME"} {
			text, err := GenerateHookScript(shell, binary)
			if err != nil || !strings.Contains(text, "hook export "+shell) {
				t.Fatalf("script=%q error=%v", text, err)
			}
			encoded, err := GenerateHookBytes(hex.EncodeToString([]byte(shell)), hex.EncodeToString([]byte(binary)))
			if err != nil || encoded != hex.EncodeToString([]byte(text)) {
				t.Fatalf("encoded=%q error=%v", encoded, err)
			}
		}
	}
	for _, args := range [][3]string{
		{"zz", "", "invalid shell hex"},
		{"0", "", "invalid shell hex"},
		{"62617368", "zz", "invalid binary hex"},
		{"62617368", "0", "invalid binary hex"},
		{"ff", "", `unsupported shell "\xff": supported shells are bash, zsh, fish`},
	} {
		value, err := GenerateHookBytes(args[0], args[1])
		if value != "" || err == nil || err.Error() != args[2] {
			t.Fatalf("value=%q error=%v want=%s", value, err, args[2])
		}
	}
	if _, err := GenerateHookScript("sh", ""); err == nil {
		t.Fatal("unsupported shell accepted")
	}
}
