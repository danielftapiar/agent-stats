package codex

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/danieltapia/agent-stats/internal/store"
)

type Indexer struct {
	db          *store.DB
	sessionsDir string
}

func NewIndexer(db *store.DB, sessionsDir string) *Indexer {
	return &Indexer{db: db, sessionsDir: sessionsDir}
}

func DefaultSessionsDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return "."
	}
	return filepath.Join(home, ".codex", "sessions")
}

func (i *Indexer) Sync(ctx context.Context) error {
	return filepath.WalkDir(i.sessionsDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if d.IsDir() || !strings.HasSuffix(path, ".jsonl") {
			return nil
		}
		return i.syncFile(ctx, path)
	})
}

func (i *Indexer) SyncActive(ctx context.Context, since time.Duration) error {
	cutoff := time.Now().Add(-since)
	return filepath.WalkDir(i.sessionsDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if d.IsDir() || !strings.HasSuffix(path, ".jsonl") {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		if info.ModTime().Before(cutoff) {
			return nil
		}
		return i.syncFileWithInfo(ctx, path, info)
	})
}

func (i *Indexer) syncFile(ctx context.Context, path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	return i.syncFileWithInfo(ctx, path, info)
}

func (i *Indexer) syncFileWithInfo(ctx context.Context, path string, info os.FileInfo) error {
	sessionID := SessionID(path)
	meta, found, err := i.db.SourceFile(ctx, path)
	if err != nil {
		return err
	}
	size := info.Size()
	modTime := info.ModTime().Unix()
	if found && meta.ProcessedOffset > 0 && usageFromSourceFile(meta).isZero() {
		if err := i.db.DeleteSourceFileEvents(ctx, path); err != nil {
			return err
		}
		meta = store.SourceFile{}
		found = false
	}
	if found && meta.SessionDir == "" {
		if err := i.db.DeleteSourceFileEvents(ctx, path); err != nil {
			return err
		}
		meta = store.SourceFile{}
		found = false
	}
	if found && meta.SizeBytes == size && meta.ModTimeUnix == modTime {
		return nil
	}
	if found && size < meta.ProcessedOffset {
		if err := i.db.DeleteSourceFileEvents(ctx, path); err != nil {
			return err
		}
		meta = store.SourceFile{}
		found = false
	}

	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()

	if meta.ProcessedOffset > 0 {
		if _, err := file.Seek(meta.ProcessedOffset, io.SeekStart); err != nil {
			return err
		}
	}

	result, err := ParseFile(file, path, sessionID, meta.ProcessedOffset, usageFromSourceFile(meta))
	if err != nil {
		return err
	}
	if len(result.Events) == 0 && result.Offset < size {
		result.Offset = size
	}
	source := store.SourceFile{
		Path:              path,
		SizeBytes:         size,
		ModTimeUnix:       modTime,
		ProcessedOffset:   result.Offset,
		SessionID:         sessionID,
		SessionDir:        result.SessionDir,
		FunctionCallCount: int64(len(result.Commands)),
	}
	if found {
		if source.SessionDir == "" {
			source.SessionDir = meta.SessionDir
		}
		source.FunctionCallCount += meta.FunctionCallCount
	}
	applyCheckpoint(&source, result.Checkpoint)
	if len(result.Events) > 0 {
		source.StartedAt = result.Events[0].Timestamp
		source.LastSeenAt = result.Events[len(result.Events)-1].Timestamp
	} else if found {
		source.StartedAt = meta.StartedAt
		source.LastSeenAt = meta.LastSeenAt
	}
	for i := range result.Commands {
		result.Commands[i].SessionDir = source.SessionDir
	}
	return i.db.SaveFileSyncWithCommands(ctx, source, result.Events, result.Commands)
}

func SessionID(path string) string {
	name := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	return strings.TrimPrefix(name, "rollout-")
}

type rawLine struct {
	Timestamp string     `json:"timestamp"`
	Type      string     `json:"type"`
	Payload   rawPayload `json:"payload"`
}

type rawPayload struct {
	Type string   `json:"type"`
	Info *rawInfo `json:"info"`
	CWD  string   `json:"cwd"`
	Name string   `json:"name"`
}

type rawInfo struct {
	TotalTokenUsage  rawUsage `json:"total_token_usage"`
	LastTokenUsage   rawUsage `json:"last_token_usage"`
	ModelContextSize int64    `json:"model_context_window"`
}

type rawUsage struct {
	InputTokens           int64 `json:"input_tokens"`
	CachedInputTokens     int64 `json:"cached_input_tokens"`
	OutputTokens          int64 `json:"output_tokens"`
	ReasoningOutputTokens int64 `json:"reasoning_output_tokens"`
	TotalTokens           int64 `json:"total_tokens"`
}

type ParseResult struct {
	Events     []store.TokenEvent
	Offset     int64
	Checkpoint rawUsage
	SessionDir string
	Commands   []store.CommandEvent
}

func ParseFile(r io.Reader, sourcePath, sessionID string, startOffset int64, initialPrevious rawUsage) (ParseResult, error) {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 1024*1024), 64*1024*1024)

	result := ParseResult{Offset: startOffset, Checkpoint: initialPrevious}
	previous := initialPrevious
	hasPrevious := !previous.isZero()

	for scanner.Scan() {
		line := scanner.Bytes()
		result.Offset += int64(len(line)) + 1
		if len(strings.TrimSpace(string(line))) == 0 {
			continue
		}

		var raw rawLine
		if err := json.Unmarshal(line, &raw); err != nil {
			continue
		}
		if raw.Type == "session_meta" && raw.Payload.CWD != "" {
			result.SessionDir = raw.Payload.CWD
			continue
		}
		if raw.Type == "response_item" && raw.Payload.Type == "function_call" {
			result.Commands = append(result.Commands, store.CommandEvent{
				SessionID:   sessionID,
				SourcePath:  sourcePath,
				Timestamp:   raw.Timestamp,
				EventType:   raw.Payload.Type,
				CommandName: commandName(raw.Payload.Name),
				SessionDir:  result.SessionDir,
			})
			continue
		}
		if raw.Payload.Type != "token_count" || raw.Payload.Info == nil {
			continue
		}

		current := normalizeTotal(raw.Payload.Info.TotalTokenUsage)
		if hasPrevious && current.equal(previous) {
			continue
		}
		usage, ok := eventUsage(raw.Payload.Info, previous, hasPrevious)
		previous = current
		result.Checkpoint = previous
		hasPrevious = true
		if !ok || usage.TotalTokens <= 0 {
			continue
		}

		result.Events = append(result.Events, store.TokenEvent{
			SessionID:             sessionID,
			SourcePath:            sourcePath,
			Timestamp:             raw.Timestamp,
			InputTokens:           usage.InputTokens,
			CachedInputTokens:     usage.CachedInputTokens,
			OutputTokens:          usage.OutputTokens,
			ReasoningOutputTokens: usage.ReasoningOutputTokens,
			TotalTokens:           usage.TotalTokens,
			ModelContextWindow:    raw.Payload.Info.ModelContextSize,
		})
	}
	if err := scanner.Err(); err != nil {
		if errors.Is(err, bufio.ErrFinalToken) {
			result.Checkpoint = previous
			return result, nil
		}
		result.Checkpoint = previous
		return result, err
	}
	result.Checkpoint = previous
	return result, nil
}

func commandName(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return "(unknown)"
	}
	return name
}

func eventUsage(info *rawInfo, previous rawUsage, hasPrevious bool) (rawUsage, bool) {
	if info.LastTokenUsage.TotalTokens > 0 ||
		info.LastTokenUsage.InputTokens > 0 ||
		info.LastTokenUsage.CachedInputTokens > 0 ||
		info.LastTokenUsage.OutputTokens > 0 ||
		info.LastTokenUsage.ReasoningOutputTokens > 0 {
		return normalizeTotal(info.LastTokenUsage), true
	}
	if !hasPrevious {
		return normalizeTotal(info.TotalTokenUsage), true
	}
	delta := rawUsage{
		InputTokens:           info.TotalTokenUsage.InputTokens - previous.InputTokens,
		CachedInputTokens:     info.TotalTokenUsage.CachedInputTokens - previous.CachedInputTokens,
		OutputTokens:          info.TotalTokenUsage.OutputTokens - previous.OutputTokens,
		ReasoningOutputTokens: info.TotalTokenUsage.ReasoningOutputTokens - previous.ReasoningOutputTokens,
		TotalTokens:           info.TotalTokenUsage.TotalTokens - previous.TotalTokens,
	}
	if delta.InputTokens < 0 || delta.CachedInputTokens < 0 || delta.OutputTokens < 0 || delta.ReasoningOutputTokens < 0 || delta.TotalTokens < 0 {
		return normalizeTotal(info.TotalTokenUsage), true
	}
	return normalizeTotal(delta), true
}

func normalizeTotal(usage rawUsage) rawUsage {
	if usage.TotalTokens == 0 {
		usage.TotalTokens = usage.InputTokens + usage.OutputTokens
	}
	return usage
}

func (usage rawUsage) equal(other rawUsage) bool {
	return usage.InputTokens == other.InputTokens &&
		usage.CachedInputTokens == other.CachedInputTokens &&
		usage.OutputTokens == other.OutputTokens &&
		usage.ReasoningOutputTokens == other.ReasoningOutputTokens &&
		usage.TotalTokens == other.TotalTokens
}

func (usage rawUsage) isZero() bool {
	return usage.InputTokens == 0 &&
		usage.CachedInputTokens == 0 &&
		usage.OutputTokens == 0 &&
		usage.ReasoningOutputTokens == 0 &&
		usage.TotalTokens == 0
}

func usageFromSourceFile(source store.SourceFile) rawUsage {
	return rawUsage{
		InputTokens:           source.LastTotalInputTokens,
		CachedInputTokens:     source.LastTotalCachedInputTokens,
		OutputTokens:          source.LastTotalOutputTokens,
		ReasoningOutputTokens: source.LastTotalReasoningTokens,
		TotalTokens:           source.LastTotalTokens,
	}
}

func applyCheckpoint(source *store.SourceFile, usage rawUsage) {
	source.LastTotalInputTokens = usage.InputTokens
	source.LastTotalCachedInputTokens = usage.CachedInputTokens
	source.LastTotalOutputTokens = usage.OutputTokens
	source.LastTotalReasoningTokens = usage.ReasoningOutputTokens
	source.LastTotalTokens = usage.TotalTokens
}
