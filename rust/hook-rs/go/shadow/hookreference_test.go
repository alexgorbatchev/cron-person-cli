package hookreference

import (
	"bufio"
	"encoding/json"
	"os"
	"testing"
)

func TestRecordedCorpusThroughShadow(t *testing.T) {
	f, err := os.Open("../../cases.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := f.Close(); err != nil {
			t.Error(err)
		}
	})
	scanner := bufio.NewScanner(f)
	count := 0
	for scanner.Scan() {
		var row struct {
			Fn    string   `json:"fn"`
			Args  []string `json:"args"`
			OK    string   `json:"ok"`
			Error string   `json:"error"`
		}
		if err := json.Unmarshal(scanner.Bytes(), &row); err != nil {
			t.Fatal(err)
		}
		var got string
		var err error
		if row.Fn == "GenerateHookBytes" {
			got, err = GenerateHookBytes(row.Args[0], row.Args[1])
		} else {
			got, err = GenerateHookScript(row.Args[0], row.Args[1])
		}
		message := ""
		if err != nil {
			message = err.Error()
		}
		if got != row.OK || message != row.Error {
			t.Fatalf("case %d: output=%q error=%q", count, got, message)
		}
		count++
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	if count != 468 || Mismatches.Load() != 0 {
		t.Fatalf("count=%d mismatches=%d", count, Mismatches.Load())
	}
	wantCalls := int64(0)
	if mode != "old" && mode != "new" {
		wantCalls = int64(count)
	}
	if Calls.Load() != wantCalls {
		t.Fatalf("calls=%d want=%d", Calls.Load(), wantCalls)
	}
}
