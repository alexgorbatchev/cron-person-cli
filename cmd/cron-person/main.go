package main

import (
	"fmt"
	"os"

	cobrahelptree "github.com/alexgorbatchev/cobra-help-tree/v2"
)

func main() {
	if err := newRootCommand().Execute(); err != nil {
		if cobrahelptree.IsAgentMode() {
			fmt.Fprintf(os.Stderr, "ERR: %v\n", err)
		} else {
			fmt.Fprintf(os.Stderr, "[ERROR] %v\n", err)
		}
		os.Exit(1)
	}
}
