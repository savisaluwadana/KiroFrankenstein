// Command parallel-sim is the headless CLI (run, replay, verify-hash, ensemble).
// It uses only the sim package: no network, no database.
package main

import (
	"fmt"
	"io"
	"os"
)

func main() { os.Exit(realMain(os.Args[1:], os.Stdout, os.Stderr)) }

func realMain(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "usage: parallel-sim run --scenario FILE --seed N")
		return 2
	}
	switch args[0] {
	case "run":
		if err := runCmd(args[1:], stdout); err != nil {
			fmt.Fprintln(stderr, "parallel-sim run:", err)
			return 1
		}
		return 0
	default:
		fmt.Fprintf(stderr, "parallel-sim: unknown command %q\n", args[0])
		return 2
	}
}
