// Command parallel-sim is the headless CLI (run, verify, ensemble). It uses only the sim
// packages: no network, no database.
//
// Exit codes: 0 success or verify match, 1 verify mismatch, 2 engine-version
// mismatch, 3 usage, I/O, invalid file, rejected input or scenario mismatch.
// llm-check (the only command that may use the network, and only with the
// provider environment set): 4 live verification pending, 5 live check failed.
package main

import (
	"fmt"
	"io"
	"os"
	"time"
)

func main() { os.Exit(run(os.Args[1:], os.Stdout, os.Stderr)) }

const usage = "usage: parallel-sim run --scenario FILE --seed N [--inputs FILE]\n" +
	"       parallel-sim verify --scenario FILE --seed N --inputs FILE --expect HEX [--tick T]\n" +
	"       parallel-sim ensemble --scenario FILE --seeds 1..20|1,2,3 --arm fixed:FILE|rulebased[@T]|adaptive:FILE[@T] [--arm ...] [--prefix FILE] [--concurrency N] [--tie-tolerance H] [--out PREFIX]\n" +
	"       parallel-sim calibrate --scenario FILE [--seeds 1..20]\n" +
	"       parallel-sim llm-check [--confirm-paid-call] [--out FILE] [--scenario FILE] [--seed N]   (env: LLM_BASE_URL LLM_API_KEY LLM_MODEL; exit 4 = pending)"

func run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, usage)
		return exitUsageOrFileError
	}
	switch args[0] {
	case "run":
		if err := runCmd(args[1:], stdout); err != nil {
			fmt.Fprintln(stderr, "parallel-sim run:", err)
			return codeOf(err)
		}
		return exitOK
	case "verify":
		code, err := verifyCmd(args[1:], stdout)
		if err != nil {
			fmt.Fprintln(stderr, "parallel-sim verify:", err)
		}
		return code
	case "ensemble":
		if err := ensembleCmd(args[1:], stdout); err != nil {
			fmt.Fprintln(stderr, "parallel-sim ensemble:", err)
			return codeOf(err)
		}
		return exitOK
	case "calibrate":
		if err := calibrateCmd(args[1:], stdout); err != nil {
			fmt.Fprintln(stderr, "parallel-sim calibrate:", err)
			return codeOf(err)
		}
		return exitOK
	case "llm-check":
		return llmCheckCmd(args[1:], os.Getenv, time.Now, nil, stdout, stderr)
	default:
		fmt.Fprintf(stderr, "parallel-sim: unknown command %q\n%s\n", args[0], usage)
		return exitUsageOrFileError
	}
}
