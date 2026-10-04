// Command reflow is the ReFlow Server command line client: JSON in, JSON out, no
// prompts, so a person at a terminal and an AI agent drive the same binary.
package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/Zijinn/ReFlow/internal/cli"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	os.Exit(cli.Run(ctx, os.Args[1:], cli.AllCommands(), os.Stdout, os.Stderr))
}
