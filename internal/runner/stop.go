package runner

import (
	"context"
	"time"

	"github.com/alexgorbatchev/cron-person-cli/internal/history"
)

const (
	// StopTimeout bounds how long StopRuns and StopTask wait: the grace
	// period a recorder gives a stopped task, plus time to notice the request
	// and record the outcome.
	StopTimeout = DefaultGrace + 5*time.Second
	// stopWaitPoll is how often a waiting stop checks whether a run ended.
	stopWaitPoll = 100 * time.Millisecond
)

// StopRuns stops the runs in ids through their recorders and waits for them,
// at most StopTimeout in all.
func StopRuns(ctx context.Context, log *history.Log, ids []string) []history.StopResult {
	return log.StopRuns(ctx, ids, StopTimeout, stopWaitPoll)
}

// StopTask stops every live run of the task taskID, as StopRuns does.
func StopTask(ctx context.Context, log *history.Log, taskID string) ([]history.StopResult, error) {
	live, err := log.Live(taskID)
	if err != nil {
		return nil, err
	}
	ids := make([]string, len(live))
	for i, run := range live {
		ids[i] = run.ID
	}
	return StopRuns(ctx, log, ids), nil
}
