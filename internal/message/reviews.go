package message

import (
	"bytes"
	"compress/gzip"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"slices"

	"github.com/charmbracelet/crush/internal/db"
	"github.com/charmbracelet/crush/internal/filechange"
	"github.com/charmbracelet/crush/internal/ui/diffreview"
)

const reviewTextBudget = 2 << 20
const reviewPayloadLimit = 16 << 20

var ErrReviewExpired = errors.New("diff details are no longer saved (only the latest five prompts keep them)")

type reviewStorage interface {
	CreateMessageWithReview(context.Context, db.CreateMessageParams, []byte) (db.Message, error)
	UpdateMessageWithReview(context.Context, db.UpdateMessageParams, []byte) error
	GetMessageReview(context.Context, string) ([]byte, error)
	PruneMessageReviews(context.Context, string) error
	LegacyReviewMessages(context.Context, string) ([]string, error)
	MigrateMessageReview(context.Context, string, string, string, []byte) error
	FinishReviewMigration(context.Context, string) error
	ReviewCommands(context.Context, string) (map[string]string, error)
}

type reviewDetail struct {
	Review   *filechange.Review `json:"review,omitempty"`
	Metadata string             `json:"metadata,omitempty"`
}

// separateReviews keeps the transcript small. Only the review drawer reads
// the compressed detail payload, and old drawers expire after five user prompts.
func separateReviews(id, sessionID string, parts []ContentPart) ([]ContentPart, []byte, error) {
	compact := slices.Clone(parts)
	details := map[string]reviewDetail{}
	budget := reviewTextBudget
	for i, part := range compact {
		result, ok := part.(ToolResult)
		if !ok || result.Review != nil && result.Review.Summary != nil {
			continue
		}
		var metadata map[string]json.RawMessage
		_ = json.Unmarshal([]byte(result.Metadata), &metadata)
		hasDiff := result.Review != nil && (len(result.Review.Changes) > 0 || result.Review.Deletions)
		for _, key := range []string{"old_content", "new_content", "diff"} {
			hasDiff = hasDiff || len(metadata[key]) > 0
		}
		if !hasDiff {
			continue
		}
		summary := &filechange.ReviewSummary{}
		if result.Review != nil {
			summary.Deletions = result.Review.Deletions
		}
		countLines(summary, result.Review, metadata)
		detail := reviewDetail{Metadata: result.Metadata}
		if result.Review != nil {
			review := *result.Review
			review.Changes = slices.Clone(review.Changes)
			for n, change := range review.Changes {
				visible := !filechange.HiddenReviewPath(change.Path, review.Root)
				if visible {
					summary.Files++
				}
				deleted := change.Before != nil && change.After == nil
				if visible && !deleted && len(summary.Paths) < 3 {
					summary.Paths = append(summary.Paths, change.Path)
				}
				if visible && change.Transfer != nil {
					switch change.Transfer.Kind {
					case "copy":
						summary.Copied++
					case "checkout":
						summary.Checkouts++
					case "move":
						summary.Moved++
					case "generated":
						summary.Generated++
					}
				}
				change.Before = boundedReviewState(change.Before, &budget)
				change.After = boundedReviewState(change.After, &budget)
				if change.Transfer != nil && change.Transfer.Baseline != nil {
					transfer := *change.Transfer
					transfer.Baseline = boundedReviewState(transfer.Baseline, &budget)
					change.Transfer = &transfer
				}
				review.Changes[n] = change
			}
			detail.Review = &review
			// These repeat the same snapshots already carried by Review.
			for _, key := range []string{"old_content", "new_content", "diff"} {
				delete(metadata, key)
			}
			if result.Metadata != "" {
				encoded, _ := json.Marshal(metadata)
				detail.Metadata = string(encoded)
			}
		} else {
			summary.Files = 1
			size := 0
			for _, key := range []string{"old_content", "new_content", "diff"} {
				size += len(metadata[key])
			}
			if size > budget {
				for _, key := range []string{"old_content", "new_content", "diff"} {
					delete(metadata, key)
				}
				metadata["review_omitted"] = json.RawMessage(`"Preview exceeds the saved diff size limit"`)
			} else {
				budget -= size
			}
			encoded, _ := json.Marshal(metadata)
			detail.Metadata = string(encoded)
		}
		for _, key := range []string{"old_content", "new_content", "diff"} {
			delete(metadata, key)
		}
		if result.Metadata != "" {
			encoded, _ := json.Marshal(metadata)
			result.Metadata = string(encoded)
		}
		result.Review = &filechange.Review{ID: id, SessionID: sessionID, Summary: summary}
		compact[i] = result
		details[result.ToolCallID] = detail
	}
	if len(details) == 0 {
		return compact, nil, nil
	}
	data, err := json.Marshal(details)
	if err != nil {
		return nil, nil, err
	}
	if len(data) > reviewPayloadLimit {
		return compact, nil, nil
	}
	var compressed bytes.Buffer
	w, _ := gzip.NewWriterLevel(&compressed, gzip.BestSpeed)
	if _, err := w.Write(data); err != nil {
		return nil, nil, err
	}
	if err := w.Close(); err != nil {
		return nil, nil, err
	}
	return compact, compressed.Bytes(), nil
}

// countLines totals the lines a result added and removed, and the files it
// deleted, by the same rules as the review drawer. It runs before large
// previews are dropped.
func countLines(summary *filechange.ReviewSummary, review *filechange.Review, metadata map[string]json.RawMessage) {
	summary.Counted = true
	if review != nil {
		var edits []diffreview.Edit
		for _, change := range review.Changes {
			if !filechange.HiddenReviewPath(change.Path, review.Root) {
				edits = append(edits, diffreview.Edit{Path: change.Path, Snapshot: &change})
			}
		}
		files := diffreview.Build(edits)
		summary.Adds, summary.Dels = diffreview.Stats(files)
		summary.Removed = diffreview.Removed(files)
		summary.NoLines = len(files) > 0 && !diffreview.AnyCounted(files)
		return
	}
	summary.Adds, summary.Dels = metadataLines(metadata)
}

func metadataLines(metadata map[string]json.RawMessage) (adds, dels int) {
	var counts struct {
		Additions int `json:"additions"`
		Removals  int `json:"removals"`
	}
	if raw, err := json.Marshal(metadata); err == nil {
		_ = json.Unmarshal(raw, &counts)
	}
	if counts.Additions+counts.Removals > 0 {
		return counts.Additions, counts.Removals
	}
	var before, after, diff string
	_ = json.Unmarshal(metadata["old_content"], &before)
	_ = json.Unmarshal(metadata["new_content"], &after)
	_ = json.Unmarshal(metadata["diff"], &diff)
	edit := diffreview.Edit{Path: "file", Before: before, After: after, Full: true}
	if before == "" && after == "" {
		edit.Unified = diff
	}
	return diffreview.Stats(diffreview.Build([]diffreview.Edit{edit}))
}

func boundedReviewState(state *filechange.State, budget *int) *filechange.State {
	if state == nil {
		return nil
	}
	copy := *state
	copy.RestoreData = ""
	if len(copy.Content) > *budget {
		copy.Content, copy.Omitted = "", "Preview exceeds the saved diff size limit"
	} else {
		*budget -= len(copy.Content)
	}
	return &copy
}

// LoadReview restores details into a temporary message, never the chat cache.
func (s *service) LoadReview(ctx context.Context, id string) (Message, error) {
	row, err := s.q.GetMessage(ctx, id)
	if err != nil {
		return Message{}, err
	}
	msg, err := s.fromDBItem(row)
	if err != nil {
		return Message{}, err
	}
	deferred := false
	for _, result := range msg.ToolResults() {
		deferred = deferred || result.Review != nil && result.Review.Summary != nil
	}
	if !deferred {
		return msg, nil
	}
	storage, ok := s.q.(reviewStorage)
	if !ok {
		return msg, nil
	}
	payload, err := storage.GetMessageReview(ctx, id)
	if errors.Is(err, sql.ErrNoRows) {
		return Message{}, ErrReviewExpired
	}
	if err != nil {
		return Message{}, err
	}
	details, err := decodeReviewDetails(payload)
	if err != nil {
		return Message{}, err
	}
	for i, part := range msg.Parts {
		if result, ok := part.(ToolResult); ok {
			if detail, ok := details[result.ToolCallID]; ok {
				result.Review, result.Metadata = detail.Review, detail.Metadata
				msg.Parts[i] = result
			}
		}
	}
	return msg, nil
}

// reviewCountStorage finds saved reviews whose summaries predate line counts.
type reviewCountStorage interface {
	UncountedReviewMessages(context.Context, string) ([]string, error)
	GetMessageReview(context.Context, string) ([]byte, error)
	MigrateMessageReview(context.Context, string, string, string, []byte) error
}

// countReviews adds line counts to older summaries while their diffs are
// still saved, so the chat can show them after the diffs expire.
func (s *service) countReviews(ctx context.Context, sessionID string) error {
	storage, ok := s.q.(reviewCountStorage)
	if !ok {
		return nil
	}
	s.reviewMu.Lock()
	defer s.reviewMu.Unlock()
	ids, err := storage.UncountedReviewMessages(ctx, sessionID)
	if err != nil {
		return err
	}
	for _, id := range ids {
		row, err := s.q.GetMessage(ctx, id)
		if err != nil {
			return err
		}
		msg, err := s.fromDBItem(row)
		if err != nil {
			return err
		}
		payload, err := storage.GetMessageReview(ctx, id)
		if errors.Is(err, sql.ErrNoRows) {
			continue
		}
		if err != nil {
			return err
		}
		details, err := decodeReviewDetails(payload)
		if err != nil {
			continue
		}
		for i, part := range msg.Parts {
			result, ok := part.(ToolResult)
			if !ok || result.Review == nil || result.Review.Summary == nil || result.Review.Summary.Counted {
				continue
			}
			detail, ok := details[result.ToolCallID]
			if !ok {
				continue
			}
			var metadata map[string]json.RawMessage
			_ = json.Unmarshal([]byte(detail.Metadata), &metadata)
			review := *result.Review
			summary := *review.Summary
			countLines(&summary, detail.Review, metadata)
			review.Summary = &summary
			result.Review = &review
			msg.Parts[i] = result
		}
		encoded, err := marshalParts(msg.Parts)
		if err != nil {
			return err
		}
		if string(encoded) == row.Parts {
			continue
		}
		if err := storage.MigrateMessageReview(ctx, id, row.Parts, string(encoded), payload); err != nil {
			return err
		}
	}
	return nil
}

func decodeReviewDetails(payload []byte) (map[string]reviewDetail, error) {
	r, err := gzip.NewReader(bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	defer r.Close()
	var details map[string]reviewDetail
	err = json.NewDecoder(io.LimitReader(r, reviewPayloadLimit+1)).Decode(&details)
	return details, err
}

// migrateReviews handles one old result at a time instead of decoding a whole
// legacy transcript with hundreds of megabytes of embedded file snapshots.
func (s *service) migrateReviews(ctx context.Context, sessionID string) error {
	storage, ok := s.q.(reviewStorage)
	if !ok {
		return nil
	}
	s.reviewMu.Lock()
	defer s.reviewMu.Unlock()
	ids, err := storage.LegacyReviewMessages(ctx, sessionID)
	if err != nil {
		return err
	}
	if ids == nil {
		return nil
	}
	if len(ids) == 0 {
		if _, err := s.q.GetSessionByID(ctx, sessionID); errors.Is(err, sql.ErrNoRows) {
			return nil
		} else if err != nil {
			return err
		}
	}
	commands, err := storage.ReviewCommands(ctx, sessionID)
	if err != nil {
		return err
	}
	for _, id := range ids {
		row, err := s.q.GetMessage(ctx, id)
		if err != nil {
			return err
		}
		msg, err := s.fromDBItem(row)
		if err != nil {
			return err
		}
		for i, part := range msg.Parts {
			result, ok := part.(ToolResult)
			if !ok || result.Review == nil || result.Review.Summary != nil {
				continue
			}
			var input struct {
				Command string `json:"command"`
			}
			_ = json.Unmarshal([]byte(commands[result.ToolCallID]), &input)
			kind := filechange.CommandTransferKind(input.Command)
			if kind == "" {
				continue
			}
			for n := range result.Review.Changes {
				change := &result.Review.Changes[n]
				if change.Before == nil && change.After != nil && change.Transfer == nil {
					change.Transfer = &filechange.Transfer{Kind: kind}
				}
			}
			msg.Parts[i] = result
		}
		parts, payload, err := separateReviews(id, sessionID, msg.Parts)
		if err != nil {
			return err
		}
		encoded, err := marshalParts(parts)
		if err != nil {
			return err
		}
		if string(encoded) == row.Parts {
			continue
		}
		if err := storage.MigrateMessageReview(ctx, id, row.Parts, string(encoded), payload); err != nil {
			return err
		}
	}
	if err := storage.PruneMessageReviews(ctx, sessionID); err != nil {
		return err
	}
	return storage.FinishReviewMigration(ctx, sessionID)
}
