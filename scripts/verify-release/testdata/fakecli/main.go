// Command fakecli stands in for cron-person in release fixtures: it prints
// the version its build injected, as cron-person --version does.
package main

import "fmt"

var version = "dev"

func main() { fmt.Println(version) }
