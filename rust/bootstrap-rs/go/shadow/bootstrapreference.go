// Package bootstrapreference runs the Go original and the Rust port of `rust/reference/bootstrap/` side by side. Generated
// by rust-anything: import it in place of github.com/alexgorbatchev/cron-person-cli/rust/reference/bootstrap. RUST_ANYTHING_MODE=old|shadow|new.
package bootstrapreference

import (
	"encoding/json"
	"log"
	"os"
	"reflect"
	"sync/atomic"

	orig "github.com/alexgorbatchev/cron-person-cli/rust/reference/bootstrap"
	port "rust-anything.local/bootstrap_rs"
)

var mode = func() string {
	if os.Getenv("RUST_ANYTHING_UNIT_BOOTSTRAP") == "original" {
		return "old"
	}
	if m := os.Getenv("RUST_ANYTHING_MODE"); m != "" {
		return m
	}
	return "shadow"
}()

// Calls and Mismatches count shadowed calls.
var Calls, Mismatches atomic.Int64

// convert moves a value between the original's and the wrapper's identical types.
func convert[T any](v any) T {
	var out T
	if text, err := json.Marshal(v); err == nil {
		_ = json.Unmarshal(text, &out)
	}
	return out
}

func compare(name string, expected, actual []any) {
	Calls.Add(1)
	if !reflect.DeepEqual(expected, actual) {
		if Mismatches.Add(1) <= 10 {
			log.Printf("[rust-anything] mismatch in %s: expected %v, got %v", name, expected, actual)
		}
	}
}

// Scenario shadows the original `Scenario`.
func Scenario(input string) (string, error) {
	if mode == "new" {
		n0, n1 := port.Scenario(input)
		return n0, n1
	}
	o0, o1 := orig.Scenario(input)
	if mode != "old" {
		func() {
			defer func() {
				if r := recover(); r != nil {
					compare("Scenario", []any{"no panic"}, []any{r})
				}
			}()
			n0, n1 := port.Scenario(input)
			compare("Scenario", []any{o0, errText(o1)}, []any{n0, errText(n1)})
		}()
	}
	return o0, o1
}

func errText(err error) any {
	if err == nil {
		return nil
	}
	return err.Error()
}
