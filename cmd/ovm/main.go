package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"

	"github.com/addodelgrossi/ovm/internal/buildinfo"
	"github.com/addodelgrossi/ovm/internal/cli"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	cmd := cli.NewRootCommand(buildinfo.Current())
	cmd.SetIn(os.Stdin)
	cmd.SetOut(os.Stdout)
	cmd.SetErr(os.Stderr)

	if err := cmd.ExecuteContext(ctx); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
