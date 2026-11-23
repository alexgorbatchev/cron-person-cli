package main

import (
	"context"
	"crypto/rand"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/alexgorbatchev/cron-person-cli/internal/cronrc"
	"github.com/alexgorbatchev/cron-person-cli/internal/store"
	"github.com/spf13/cobra"
)

//go:embed dashboard-assets
var dashboardAssets embed.FS

type dashboard struct {
	store *store.Store
	token string
	mu    sync.Mutex
}
type dashboardDocument struct {
	Dir      string          `json:"dir"`
	Content  string          `json:"content"`
	Revision string          `json:"revision"`
	Status   store.DirStatus `json:"status"`
}

func newDashboardCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "dashboard", Short: "Serve the bundled local CRUD dashboard", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, args []string) error {
		st, err := getStore(cmd)
		if err != nil {
			return err
		}
		port, _ := cmd.Flags().GetInt("port")
		listener, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
		if err != nil {
			return err
		}
		defer listener.Close()
		d := &dashboard{store: st, token: rand.Text()}
		server := &http.Server{Handler: d.handler(), ReadHeaderTimeout: 5 * time.Second}
		ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt)
		defer stop()
		if err := writeOutput(cmd, fmt.Sprintf("url: http://%s/#%s\n", listener.Addr(), d.token)); err != nil {
			return err
		}
		done := make(chan struct{})
		defer close(done)
		go func() {
			select {
			case <-ctx.Done():
				shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				_ = server.Shutdown(shutdown)
			case <-done:
			}
		}()
		err = server.Serve(listener)
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}}
	cmd.Flags().Int("port", 8080, "Local dashboard port (0 chooses an available port)")
	return cmd
}

func (d *dashboard) handler() http.Handler {
	mux := http.NewServeMux()
	assets, _ := fs.Sub(dashboardAssets, "dashboard-assets")
	mux.Handle("/", http.FileServer(http.FS(assets)))
	mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		if r.Header.Get("X-Dashboard-Token") != d.token {
			http.Error(w, "unauthorized", 401)
			return
		}
		if origin := r.Header.Get("Origin"); origin != "" && origin != "http://"+r.Host {
			http.Error(w, "foreign origin", 403)
			return
		}
		d.mu.Lock()
		defer d.mu.Unlock()
		if err := d.api(w, r); err != nil {
			http.Error(w, err.Error(), 400)
		}
	})
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; frame-ancestors 'none'")
		mux.ServeHTTP(w, r)
	})
}

func (d *dashboard) api(w http.ResponseWriter, r *http.Request) error {
	w.Header().Set("Content-Type", "application/json")
	if r.URL.Path == "/api/directories" && r.Method == http.MethodGet {
		records, err := d.store.List()
		if err != nil {
			return err
		}
		return json.NewEncoder(w).Encode(records)
	}
	if r.URL.Path != "/api/document" {
		http.Error(w, "not found", 404)
		return nil
	}
	if r.Method == http.MethodGet {
		dir := r.URL.Query().Get("dir")
		if !filepath.IsAbs(dir) {
			return errors.New("directory must be absolute")
		}
		status, _, data, err := d.store.Inspect(dir)
		if err != nil {
			return err
		}
		return json.NewEncoder(w).Encode(dashboardDocument{Dir: dir, Content: string(data), Revision: cronrc.ComputeHash(data), Status: status})
	}
	if r.Method != http.MethodPut && r.Method != http.MethodPost && r.Method != http.MethodDelete {
		http.Error(w, "method not allowed", 405)
		return nil
	}
	var doc dashboardDocument
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	if err := json.NewDecoder(r.Body).Decode(&doc); err != nil {
		return err
	}
	if !filepath.IsAbs(doc.Dir) {
		return errors.New("directory must be absolute")
	}
	path := filepath.Join(doc.Dir, ".cronrc")
	status, _, current, err := d.store.Inspect(doc.Dir)
	if err != nil {
		return err
	}
	if cronrc.ComputeHash(current) != doc.Revision {
		http.Error(w, "file changed; reload before saving", 409)
		return nil
	}
	switch r.Method {
	case http.MethodPost:
		if doc.Status == "allowed" {
			_, err = d.store.Allow(doc.Dir)
		} else if doc.Status == "denied" {
			err = d.store.Deny(doc.Dir)
		} else {
			return errors.New("expected allowed or denied status")
		}
	case http.MethodDelete:
		info, statErr := os.Lstat(path)
		if statErr != nil {
			return statErr
		}
		if !info.Mode().IsRegular() {
			return errors.New("cronrc must be a regular file")
		}
		// Revoke first so a failed deletion never leaves an approved task behind.
		if err = d.store.Deny(doc.Dir); err == nil {
			err = os.Remove(path)
		}
	case http.MethodPut:
		if _, err := cronrc.Parse(strings.NewReader(doc.Content), path); err != nil {
			return err
		}
		if status == store.StatusMissingCronrc || status == store.StatusOrphan {
			var f *os.File
			f, err = os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
			if err == nil {
				_, err = f.WriteString(doc.Content)
				err = errors.Join(err, f.Close())
			}
		} else {
			// Never follow a symbolic link while replacing a configuration.
			info, statErr := os.Lstat(path)
			if statErr != nil {
				return statErr
			}
			if !info.Mode().IsRegular() {
				return errors.New("cronrc must be a regular file")
			}
			var f *os.File
			f, err = os.CreateTemp(doc.Dir, ".cronrc-dashboard-*")
			if err != nil {
				return err
			}
			defer os.Remove(f.Name())
			_, err = f.WriteString(doc.Content)
			err = errors.Join(err, f.Close())
			if err == nil {
				err = os.Chmod(f.Name(), info.Mode().Perm())
			}
			if err == nil {
				err = os.Rename(f.Name(), path)
			}
		}
	}
	if err != nil {
		return err
	}
	return json.NewEncoder(w).Encode(map[string]bool{"ok": true})
}
