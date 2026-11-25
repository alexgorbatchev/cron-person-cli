package bootstrapreference

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestRecordedCorpus(t *testing.T) {
	file, err := os.Open(filepath.Join("..", "..", "cases.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := file.Close(); err != nil {
			t.Error(err)
		}
	})
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 4096), 4*1024*1024)
	count := 0
	for scanner.Scan() {
		var record struct {
			Args  []string `json:"args"`
			OK    string   `json:"ok"`
			Error string   `json:"error"`
		}
		if err := json.Unmarshal(scanner.Bytes(), &record); err != nil {
			t.Fatal(err)
		}
		if len(record.Args) != 1 {
			t.Fatalf("case %d has %d arguments", count, len(record.Args))
		}
		actual, err := Scenario(record.Args[0])
		if record.Error != "" {
			if err == nil || err.Error() != record.Error || actual != "" {
				t.Fatalf("case %d: got (%q, %v), want error %q", count, actual, err, record.Error)
			}
		} else if err != nil || actual != record.OK {
			t.Fatalf("case %d differs from recorded Go observation: (%q, %v)", count, actual, err)
		}
		count++
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	if count != 169 {
		t.Fatalf("unexpected corpus count %d", count)
	}
	expectedCalls := int64(0)
	if mode != "old" && mode != "new" {
		expectedCalls = int64(count)
	}
	if Calls.Load() != expectedCalls || Mismatches.Load() != 0 {
		t.Fatalf("calls=%d mismatches=%d", Calls.Load(), Mismatches.Load())
	}
	t.Logf("mode=%s observations=%d shadow_calls=%d mismatches=%d", mode, count, Calls.Load(), Mismatches.Load())
}
