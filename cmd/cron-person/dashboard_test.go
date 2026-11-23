package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/alexgorbatchev/cron-person-cli/internal/cronrc"
	"github.com/alexgorbatchev/cron-person-cli/internal/store"
)

func TestDashboardCommand(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	timer := time.AfterFunc(100*time.Millisecond, cancel)
	defer timer.Stop()
	defer cancel()
	cmd := newRootCommand()
	cmd.SetContext(ctx)
	var output bytes.Buffer
	cmd.SetOut(&output)
	cmd.SetArgs([]string{"--store", filepath.Join(t.TempDir(), "allowed.json"), "dashboard", "--port", "0"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "url: http://127.0.0.1:") {
		t.Fatal(output.String())
	}
}

func TestDashboardCRUD(t *testing.T) {
	dir := t.TempDir()
	st, err := store.NewStore(filepath.Join(t.TempDir(), "allowed.json"))
	if err != nil {
		t.Fatal(err)
	}
	d := &dashboard{store: st, token: "session"}
	handler := d.handler()
	call := func(method, path string, body interface{}, want int) []byte {
		t.Helper()
		var data []byte
		if body != nil {
			data, err = json.Marshal(body)
			if err != nil {
				t.Fatal(err)
			}
		}
		r := httptest.NewRequest(method, path, bytes.NewReader(data))
		r.Header.Set("X-Dashboard-Token", "session")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if w.Code != want {
			t.Fatalf("%s %s: %d %s", method, path, w.Code, w.Body.String())
		}
		return w.Body.Bytes()
	}
	call("GET", "/", nil, 200)
	call("GET", "/api/directories", nil, 200)
	call("GET", "/api/unknown", nil, 404)
	call("PATCH", "/api/document", nil, 405)
	call("GET", "/api/document?dir=relative", nil, 400)
	call("PUT", "/api/document", dashboardDocument{Dir: "relative"}, 400)
	call("PUT", "/api/document", nil, 400)
	doc := dashboardDocument{Dir: dir, Revision: cronrc.ComputeHash(nil), Content: "@daily echo hello\n"}
	call("GET", "/api/document?dir="+dir, nil, 200)
	call("PUT", "/api/document", doc, 200)
	call("PUT", "/api/document", doc, 409)
	load := func() {
		t.Helper()
		if err := json.Unmarshal(call("GET", "/api/document?dir="+dir, nil, 200), &doc); err != nil {
			t.Fatal(err)
		}
	}
	load()
	if doc.Content != "@daily echo hello\n" {
		t.Fatal(doc)
	}
	doc.Status = "allowed"
	call("POST", "/api/document", doc, 200)
	call("GET", "/api/directories", nil, 200)
	load()
	if doc.Status != store.StatusAllowed {
		t.Fatal(doc)
	}
	doc.Content = "invalid"
	call("PUT", "/api/document", doc, 400)
	doc.Content = "@hourly echo updated\n"
	call("PUT", "/api/document", doc, 200)
	load()
	if doc.Status != store.StatusChanged {
		t.Fatal(doc)
	}
	doc.Status = "denied"
	call("POST", "/api/document", doc, 200)
	doc.Status = "invalid"
	call("POST", "/api/document", doc, 400)
	call("DELETE", "/api/document", doc, 200)
	if _, err := os.Stat(filepath.Join(dir, ".cronrc")); !os.IsNotExist(err) {
		t.Fatalf("configuration remains: %v", err)
	}
	call("PUT", "/api/document", dashboardDocument{Dir: filepath.Join(dir, "absent"), Revision: cronrc.ComputeHash(nil)}, 400)
}

func TestDashboardSecurity(t *testing.T) {
	d := &dashboard{token: "session"}
	for _, test := range []struct {
		token, origin string
		want          int
	}{
		{"", "", 401}, {"wrong", "", 401}, {"session", "https://foreign.example", 403},
	} {
		r := httptest.NewRequest("GET", "http://localhost/api/directories", nil)
		r.Header.Set("X-Dashboard-Token", test.token)
		r.Header.Set("Origin", test.origin)
		w := httptest.NewRecorder()
		d.handler().ServeHTTP(w, r)
		if w.Code != test.want {
			t.Fatalf("got %d", w.Code)
		}
		if !strings.Contains(w.Header().Get("Content-Security-Policy"), "frame-ancestors 'none'") {
			t.Fatal("missing frame protection")
		}
	}
}
