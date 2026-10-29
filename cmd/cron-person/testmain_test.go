package main

import (
	"fmt"
	"os"
	"testing"
)

// asCLIEnv makes a re-executed test binary behave as cron-person, which is how
// a foreground `task run` under test starts its recorder.
const asCLIEnv = "CRON_PERSON_TEST_AS_CLI"

func TestMain(m *testing.M) {
	if os.Getenv(asCLIEnv) == "1" {
		os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
	}
	if err := os.Setenv(asCLIEnv, "1"); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	// Tests must never read the developer's terminal.
	devNull, err := os.Open(os.DevNull)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	os.Stdin = devNull
	os.Exit(m.Run())
}
