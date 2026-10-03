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
)

const reviewTextBudget = 2 << 20
const reviewPayloadLimit = 16 << 20

var ErrReviewExpired = errors.New("diff details are no longer saved (only the latest five replies are retained)")

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
// the compressed detail payload, and old drawers expire after five replies.
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
		hasDiff := result.Review != nil && len(result.Review.Changes) > 0
		for _, key := range []string{"old_content", "new_content", "diff"} {
			hasDiff = hasDiff || len(metadata[key]) > 0
		}
		if !hasDiff {
			continue
		}
		summary := &filechange.ReviewSummary{}
		detail := reviewDetail{Metadata: result.Metadata}
		if result.Review != nil {
			review := *result.Review
			review.Changes = slices.Clone(review.Changes)
			for n, change := range review.Changes {
				visible := !filechange.HiddenReviewPath(change.Path, review.Root)
				if visible {
					summary.Files++
				}
				if visible && len(summary.Paths) < 3 {
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

func boundedReviewState(state *filechange.State, budget *int) *filechange.State {
	if state == nil {
		return nil
	}
	copy := *state
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
	r, err := gzip.NewReader(bytes.NewReader(payload))
	if err != nil {
		return Message{}, err
	}
	defer r.Close()
	var details map[string]reviewDetail
	if err := json.NewDecoder(io.LimitReader(r, reviewPayloadLimit+1)).Decode(&details); err != nil {
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
