package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/danieltapia/agent-stats/internal/codex"
	"github.com/danieltapia/agent-stats/internal/store"
	"github.com/danieltapia/agent-stats/internal/tui"
	"github.com/danieltapia/agent-stats/internal/views"
)

type options struct {
	sessionsDir string
	cachePath   string
	jsonOutput  bool
	limit       int
	sessionID   string
}

func Run(ctx context.Context, args []string, out io.Writer) error {
	opts, cmd, err := parseArgs(args)
	if err != nil {
		return err
	}
	if cmd == "help" {
		printHelp(out)
		return nil
	}

	db, err := store.Open(opts.cachePath)
	if err != nil {
		return err
	}
	defer db.Close()

	indexer := codex.NewIndexer(db, opts.sessionsDir)
	if err := indexer.Sync(ctx); err != nil {
		return err
	}

	switch cmd {
	case "", "tui":
		return tui.Run(ctx, db, indexer)
	case "summary", "today", "sessions", "commands", "payload", "tokens", "top", "graph":
		return printView(ctx, out, db, cmd, opts)
	case "export":
		return exportJSON(ctx, out, db)
	default:
		return fmt.Errorf("unknown command %q", cmd)
	}
}

func parseArgs(args []string) (options, string, error) {
	opts := options{
		sessionsDir: codex.DefaultSessionsDir(),
		cachePath:   store.DefaultCachePath(),
		limit:       20,
	}
	cmd := ""
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch arg {
		case "--sessions-dir":
			i++
			if i >= len(args) {
				return opts, cmd, errors.New("--sessions-dir requires a value")
			}
			opts.sessionsDir = expandHome(args[i])
		case "--cache":
			i++
			if i >= len(args) {
				return opts, cmd, errors.New("--cache requires a value")
			}
			opts.cachePath = expandHome(args[i])
		case "--json":
			opts.jsonOutput = true
		case "--limit":
			i++
			if i >= len(args) {
				return opts, cmd, errors.New("--limit requires a value")
			}
			n, err := strconv.Atoi(args[i])
			if err != nil || n <= 0 {
				return opts, cmd, fmt.Errorf("invalid --limit value %q", args[i])
			}
			opts.limit = n
		case "--help", "-h", "help":
			return opts, "help", nil
		default:
			if strings.HasPrefix(arg, "-") {
				return opts, cmd, fmt.Errorf("unknown flag %q", arg)
			}
			if cmd == "" {
				cmd = arg
			} else if cmd == "payload" {
				opts.sessionID = arg
			}
		}
	}
	return opts, cmd, nil
}

func printView(ctx context.Context, out io.Writer, db *store.DB, cmd string, opts options) error {
	if cmd == "graph" {
		cmd = "today"
	}
	var (
		data views.Data
		err  error
	)
	if cmd == "payload" && opts.sessionID != "" {
		data, err = views.LoadSessionPayload(ctx, db, opts.sessionID, opts.limit)
	} else {
		data, err = views.Load(ctx, db, cmd, opts.limit, time.Now())
	}
	if err != nil {
		return err
	}
	if opts.jsonOutput {
		return json.NewEncoder(out).Encode(data)
	}
	fmt.Fprint(out, views.Render(data, cmd))
	return nil
}

func exportJSON(ctx context.Context, out io.Writer, db *store.DB) error {
	events, err := db.Events(ctx)
	if err != nil {
		return err
	}
	enc := json.NewEncoder(out)
	enc.SetIndent("", "  ")
	return enc.Encode(events)
}

func printHelp(out io.Writer) {
	fmt.Fprint(out, `agent-stats reads local Codex token usage.

Usage:
  agent-stats [tui]
  agent-stats summary [--json]
  agent-stats today [--json]
  agent-stats sessions [--limit 20]
  agent-stats commands
  agent-stats payload [session-id]
  agent-stats tokens
  agent-stats top [--limit 20]
  agent-stats export

Flags:
  --sessions-dir PATH  Codex sessions directory
  --cache PATH         SQLite cache path
  --json               Print command output as JSON
  --limit N            Limit ranked views
`)
}

func expandHome(path string) string {
	if path == "~" {
		if home, err := os.UserHomeDir(); err == nil {
			return home
		}
	}
	if strings.HasPrefix(path, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, strings.TrimPrefix(path, "~/"))
		}
	}
	return path
}
