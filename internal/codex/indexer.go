package codex

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strconv"
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
	if found && meta.Model == "" {
		if err := i.db.DeleteSourceFileEvents(ctx, path); err != nil {
			return err
		}
		meta = store.SourceFile{}
		found = false
	}
	if found {
		hasPayloads, err := i.db.HasPayloadEvents(ctx, path)
		if err != nil {
			return err
		}
		if !hasPayloads {
			if err := i.db.DeleteSourceFileEvents(ctx, path); err != nil {
				return err
			}
			meta = store.SourceFile{}
			found = false
		}
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

	result, err := ParseFile(file, path, sessionID, meta.ProcessedOffset, usageFromSourceFile(meta), meta.Model)
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
		Model:             result.Model,
		FunctionCallCount: int64(len(result.Commands)),
	}
	if found {
		if source.SessionDir == "" {
			source.SessionDir = meta.SessionDir
		}
		if source.Model == "" {
			source.Model = meta.Model
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
	return i.db.SaveFileSyncWithDetails(ctx, source, result.Events, result.Commands, result.Payloads)
}

func SessionID(path string) string {
	name := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	return strings.TrimPrefix(name, "rollout-")
}

type rawLine struct {
	Timestamp string          `json:"timestamp"`
	Type      string          `json:"type"`
	Payload   json.RawMessage `json:"payload"`
}

type rawPayload struct {
	Type               string          `json:"type"`
	Info               *rawInfo        `json:"info"`
	CWD                string          `json:"cwd"`
	Model              string          `json:"model"`
	Name               string          `json:"name"`
	Namespace          string          `json:"namespace"`
	Arguments          string          `json:"arguments"`
	Phase              string          `json:"phase"`
	CompletedAt        string          `json:"completed_at"`
	DurationMS         json.RawMessage `json:"duration_ms"`
	TimeToFirstTokenMS json.RawMessage `json:"time_to_first_token_ms"`
	CallID             string          `json:"call_id"`
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
	Model      string
	Commands   []store.CommandEvent
	Payloads   []store.PayloadEvent
}

func ParseFile(r io.Reader, sourcePath, sessionID string, startOffset int64, initialPrevious rawUsage, initialModel string) (ParseResult, error) {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 1024*1024), 64*1024*1024)

	result := ParseResult{Offset: startOffset, Checkpoint: initialPrevious, Model: initialModel}
	previous := initialPrevious
	hasPrevious := !previous.isZero()
	currentModel := initialModel

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
		var payload rawPayload
		if len(raw.Payload) > 0 {
			if err := json.Unmarshal(raw.Payload, &payload); err != nil {
				payload = rawPayload{}
			}
		}
		if raw.Type == "turn_context" && payload.Model != "" {
			currentModel = payload.Model
			result.Model = payload.Model
		}
		payloadEvent := store.PayloadEvent{
			SessionID:          sessionID,
			SourcePath:         sourcePath,
			Timestamp:          raw.Timestamp,
			TopLevelType:       raw.Type,
			PayloadType:        payload.Type,
			Phase:              payload.Phase,
			PayloadBytes:       int64(len(raw.Payload)),
			CompletedAt:        payload.CompletedAt,
			DurationMS:         rawInt64(payload.DurationMS),
			TimeToFirstTokenMS: rawInt64(payload.TimeToFirstTokenMS),
			CommandName:        commandName(payload.Name),
			NormalizedCommand:  normalizedPayloadCommand(payload),
			CallID:             payload.CallID,
			Model:              currentModel,
			PayloadJSON:        string(raw.Payload),
			RawJSON:            string(line),
		}
		if payloadEvent.PayloadType == "" {
			payloadEvent.PayloadType = "(none)"
		}
		if payloadEvent.CommandName == "(unknown)" {
			payloadEvent.CommandName = ""
		}
		if raw.Type == "session_meta" && payload.CWD != "" {
			result.SessionDir = payload.CWD
			result.Payloads = append(result.Payloads, payloadEvent)
			continue
		}
		if raw.Type == "response_item" && payload.Type == "function_call" {
			result.Commands = append(result.Commands, store.CommandEvent{
				SessionID:   sessionID,
				SourcePath:  sourcePath,
				Timestamp:   raw.Timestamp,
				EventType:   payload.Type,
				CommandName: commandName(payload.Name),
				SessionDir:  result.SessionDir,
			})
			result.Payloads = append(result.Payloads, payloadEvent)
			continue
		}
		if payload.Type != "token_count" || payload.Info == nil {
			result.Payloads = append(result.Payloads, payloadEvent)
			continue
		}

		current := normalizeTotal(payload.Info.TotalTokenUsage)
		if hasPrevious && current.equal(previous) {
			result.Payloads = append(result.Payloads, payloadEvent)
			continue
		}
		usage, ok := eventUsage(payload.Info, previous, hasPrevious)
		previous = current
		result.Checkpoint = previous
		hasPrevious = true
		payloadEvent.InputTokens = usage.InputTokens
		payloadEvent.CachedInputTokens = usage.CachedInputTokens
		payloadEvent.OutputTokens = usage.OutputTokens
		payloadEvent.ReasoningOutputTokens = usage.ReasoningOutputTokens
		payloadEvent.TotalTokens = usage.TotalTokens
		payloadEvent.ModelContextWindow = payload.Info.ModelContextSize
		payloadEvent.Model = currentModel
		result.Payloads = append(result.Payloads, payloadEvent)
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
			ModelContextWindow:    payload.Info.ModelContextSize,
			Model:                 currentModel,
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

func rawInt64(raw json.RawMessage) int64 {
	if len(raw) == 0 || string(raw) == "null" {
		return 0
	}
	var n int64
	if err := json.Unmarshal(raw, &n); err == nil {
		return n
	}
	var f float64
	if err := json.Unmarshal(raw, &f); err == nil {
		return int64(f)
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		if parsed, err := strconv.ParseInt(s, 10, 64); err == nil {
			return parsed
		}
	}
	return 0
}

func normalizedPayloadCommand(payload rawPayload) string {
	if payload.Type != "function_call" || payload.Name == "" {
		return ""
	}
	if payload.Name != "exec_command" {
		return payload.Name
	}
	var args struct {
		Cmd string `json:"cmd"`
	}
	if err := json.Unmarshal([]byte(payload.Arguments), &args); err != nil {
		return payload.Name
	}
	return normalizeShellCommand(args.Cmd)
}

func normalizeShellCommand(cmd string) string {
	fields := strings.Fields(cmd)
	for len(fields) > 0 && (fields[0] == "rtk" || strings.HasPrefix(fields[0], "rtk=")) {
		fields = fields[1:]
	}
	if len(fields) == 0 {
		return ""
	}
	base := filepath.Base(fields[0])
	if base == "" {
		return fields[0]
	}
	return base
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
