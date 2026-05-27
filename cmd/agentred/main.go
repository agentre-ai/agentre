// Package main is the agentred binary entry point.
package main

import "os"

func main() {
	os.Exit(execute())
}

// execute runs the root command and translates errors into shell exit codes:
//
//	0 — success
//	1 — runtime error (daemon unreachable, IO failure, …)
//	2 — usage error (bad flag, missing arg, unknown subcommand)
func execute() int {
	if err := newRootCmd().Execute(); err != nil {
		if _, ok := err.(*usageError); ok {
			return 2
		}
		return 1
	}
	return 0
}
