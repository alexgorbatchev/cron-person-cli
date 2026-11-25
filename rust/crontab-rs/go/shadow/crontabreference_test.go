package crontabreference

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func TestRecordedCalls(t *testing.T) {
	data, err := os.ReadFile("../../cases.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		var recorded struct {
			Function string   `json:"fn"`
			Args     []string `json:"args"`
			OK       string   `json:"ok"`
			Error    *string  `json:"error"`
		}
		if err := json.Unmarshal([]byte(line), &recorded); err != nil {
			t.Fatal(err)
		}
		var output string
		var callErr error
		switch recorded.Function {
		case "Text":
			output, callErr = Text(recorded.Args[0], recorded.Args[1], recorded.Args[2])
		case "Process":
			output, callErr = Process(recorded.Args[0])
		default:
			t.Fatalf("unknown function %s", recorded.Function)
		}
		if recorded.Error != nil {
			if callErr == nil || callErr.Error() != *recorded.Error {
				t.Fatalf("case %d: %v", count, callErr)
			}
		} else if callErr != nil || output != recorded.OK {
			t.Fatalf("case %d: output differs, error %v", count, callErr)
		}
		count++
	}
	if count != 1793 {
		t.Fatalf("case count %d", count)
	}
	if Mismatches.Load() != 0 {
		t.Fatalf("shadow mismatches %d", Mismatches.Load())
	}
	if mode == "shadow" && Calls.Load() != int64(count) {
		t.Fatalf("shadowed %d/%d", Calls.Load(), count)
	}
	t.Logf("mode=%s exact=%d mismatches=%d", mode, count, Mismatches.Load())
}
