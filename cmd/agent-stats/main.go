package main

import (
	"context"
	"fmt"
	"os"

	"github.com/danieltapia/agent-stats/internal/app"
)

func main() {
	if err := app.Run(context.Background(), os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
