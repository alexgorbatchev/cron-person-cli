package main

import (
	"bytes"
	"encoding/json"
	"io"
	"strings"
	"testing"
)

type failedWriter struct{}

func (failedWriter) Write([]byte) (int, error) { return 0, io.ErrClosedPipe }

func TestProbeProtocol(t *testing.T) {
	for _, tc := range []struct {
		name, input, want string
		output            io.Writer
		code              int
	}{
		{"bytes", `{"fn":"Text","args":["merge","ff","00"]}`, `{"ok":"ff0a0a000a"}`, nil, 0},
		{"reference error", `{"fn":"Process","args":["{}"]}`, `{"error":"unknown process operation"}`, nil, 0},
		{"unknown function", `{"fn":"unknown","args":[]}`, "", nil, 1},
		{"missing text arguments", `{"fn":"Text","args":[]}`, "", nil, 1},
		{"missing process arguments", `{"fn":"Process","args":[]}`, "", nil, 1},
		{"invalid JSON", `{`, "", nil, 1},
		{"output failure", `{"fn":"Text","args":["remove","",""]}`, "", failedWriter{}, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var output, diagnostic bytes.Buffer
			writer := tc.output
			if writer == nil {
				writer = &output
			}
			if code := run(strings.NewReader(tc.input), writer, &diagnostic); code != tc.code {
				t.Fatalf("exit %d, want %d: %s", code, tc.code, &diagnostic)
			}
			if tc.want != "" {
				var got, want map[string]string
				if err := json.Unmarshal(output.Bytes(), &got); err != nil {
					t.Fatal(err)
				}
				if err := json.Unmarshal([]byte(tc.want), &want); err != nil {
					t.Fatal(err)
				}
				if len(got) != len(want) || got["ok"] != want["ok"] || got["error"] != want["error"] {
					t.Fatalf("got %v, want %v", got, want)
				}
			}
			if tc.code != 0 && diagnostic.Len() == 0 {
				t.Fatal("missing failure diagnostic")
			}
		})
	}
}
