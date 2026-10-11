package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/addodelgrossi/ovm/internal/buildinfo"
	"github.com/addodelgrossi/ovm/internal/cli"
)

func main() {
	os.Exit(run())
}

// run executes the CLI and returns a stable process exit code. It is a separate
// function so deferred cleanup runs before the process exits and so the exit
// code mapping is unit-testable through cli.ExitCodeForError.
func run() int {
	// Handle interrupt and, where portable, termination so a long-running
	// discovery or inventory round cancels cleanly and maps to the cancellation
	// exit code.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	cmd := cli.NewRootCommand(buildinfo.Current(), cli.Default(), os.Args[1:])
	cmd.SetIn(os.Stdin)
	cmd.SetOut(os.Stdout)
	cmd.SetErr(os.Stderr)

	if err := cmd.ExecuteContext(ctx); err != nil {
		// Print the safe error exactly once. Application errors already carry a
		// credential-free message; other errors are printed as-is because the
		// lower layers never place secrets in their error text.
		//
		// A presented error has already been explained to the user by the
		// command (for example a discovery diagnostic written to stderr before
		// the failure was known to be fatal), so it is not printed again — this
		// keeps the "exactly once" guarantee without losing the exit code.
		if !cli.IsPresented(err) {
			_, _ = fmt.Fprintln(os.Stderr, err)
		}
		return cli.ExitCodeForError(err)
	}
	return cli.ExitOK
}
