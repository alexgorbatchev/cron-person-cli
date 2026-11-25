// Command probe replays real reference calls with Go's native coverage instrumentation.
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"

	reference "github.com/alexgorbatchev/cron-person-cli/rust/reference/crontab"
)

func main() {
	os.Exit(run(os.Stdin, os.Stdout, os.Stderr))
}

func run(input io.Reader, output, errors io.Writer) int {
	decoder := json.NewDecoder(input)
	encoder := json.NewEncoder(output)
	for {
		var call struct {
			Function string   `json:"fn"`
			Args     []string `json:"args"`
		}
		if err := decoder.Decode(&call); err == io.EOF {
			return 0
		} else if err != nil {
			fmt.Fprintln(errors, err)
			return 1
		}
		var value string
		var err error
		switch call.Function {
		case "Text":
			if len(call.Args) != 3 {
				fmt.Fprintln(errors, "Text requires three arguments")
				return 1
			}
			value, err = reference.Text(call.Args[0], call.Args[1], call.Args[2])
		case "Process":
			if len(call.Args) != 1 {
				fmt.Fprintln(errors, "Process requires one argument")
				return 1
			}
			value, err = reference.Process(call.Args[0])
		default:
			fmt.Fprintln(errors, "unknown reference call")
			return 1
		}
		result := map[string]string{"ok": value}
		if err != nil {
			result = map[string]string{"error": err.Error()}
		}
		if err := encoder.Encode(result); err != nil {
			fmt.Fprintln(errors, err)
			return 1
		}
	}
}
