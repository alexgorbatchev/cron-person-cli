package runner

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/alexgorbatchev/cron-person-cli/internal/history"
)

func TestStopTaskStopsEveryLiveRunOfTheTask(t *testing.T) {
	log := history.New(t.TempDir())
	var recs []*history.Recording
	for _, command := range []string{"one", "one", "two"} {
		rec, err := log.Start(history.StartOptions{Dir: "/srv/a", Command: command, Trigger: history.TriggerCron})
		if err != nil {
			t.Fatal(err)
		}
		recs = append(recs, rec)
	}
	t.Cleanup(func() { _ = recs[2].Finish(nil) })
	for _, rec := range recs[:2] {
		go func() {
			for {
				if requested, _ := rec.StopRequested(); requested {
					rec.Stopped()
					_ = rec.Finish(nil)
					return
				}
				time.Sleep(time.Millisecond)
			}
		}()
	}

	results, err := StopTask(t.Context(), log, history.TaskID("/srv/a", "one"))
	if err != nil || len(results) != 2 {
		t.Fatalf("StopTask = %+v, %v", results, err)
	}
	for _, r := range results {
		if r.Err != nil || r.Run.Status != history.StatusStopped {
			t.Fatalf("result = %+v", r)
		}
	}
	if live, err := log.Live(history.TaskID("/srv/a", "two")); err != nil || len(live) != 1 {
		t.Fatalf("another task's run was touched: %+v, %v", live, err)
	}
	if results, err := StopTask(t.Context(), log, history.TaskID("/srv/a", "none")); err != nil || len(results) != 0 {
		t.Fatalf("StopTask without live runs = %+v, %v", results, err)
	}
}

func TestStopTaskReportsUnreadableHistory(t *testing.T) {
	root := filepath.Join(t.TempDir(), "runs")
	if err := os.WriteFile(root, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := StopTask(t.Context(), history.New(root), "abc"); err == nil {
		t.Fatal("StopTask ignored unreadable history")
	}
}
