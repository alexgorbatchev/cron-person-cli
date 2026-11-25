// Package crontabreference runs the Go original and the Rust port of `rust/reference/crontab/` side by side. Generated
// by rust-anything: import it in place of github.com/alexgorbatchev/cron-person-cli/rust/reference/crontab. RUST_ANYTHING_MODE=old|shadow|new.
package crontabreference

import (
	"encoding/json"
	"log"
	"os"
	"reflect"
	"sync/atomic"

	orig "github.com/alexgorbatchev/cron-person-cli/rust/reference/crontab"
	port "rust-anything.local/crontab_rs"
)

var mode = func() string {
	if os.Getenv("RUST_ANYTHING_UNIT_CRONTAB") == "original" {
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

// Text shadows the original `Text`.
func Text(operation string, existing string, managed string) (string, error) {
	if mode == "new" {
		n0, n1 := port.Text(operation, existing, managed)
		return n0, n1
	}
	o0, o1 := orig.Text(operation, existing, managed)
	if mode != "old" {
		func() {
			defer func() {
				if r := recover(); r != nil {
					compare("Text", []any{"no panic"}, []any{r})
				}
			}()
			n0, n1 := port.Text(operation, existing, managed)
			compare("Text", []any{o0, errText(o1)}, []any{n0, errText(n1)})
		}()
	}
	return o0, o1
}

// Process shadows the original `Process`.
func Process(input string) (string, error) {
	if mode == "new" {
		n0, n1 := port.Process(input)
		return n0, n1
	}
	o0, o1 := orig.Process(input)
	if mode != "old" {
		func() {
			defer func() {
				if r := recover(); r != nil {
					compare("Process", []any{"no panic"}, []any{r})
				}
			}()
			n0, n1 := port.Process(input)
			compare("Process", []any{o0, errText(o1)}, []any{n0, errText(n1)})
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
