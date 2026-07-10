package adminops

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
	"github.com/google/uuid"

	"remnawave-tg-shop-bot/internal/config"
	"remnawave-tg-shop-bot/internal/database"
)

// broadcastJobs tracks in-flight/completed broadcast jobs by ID. In-memory only — progress is
// lost on restart, but the final counters are already durable in admin_audit_log by then. Values
// are BroadcastProgress (not pointers): each update Stores a fresh snapshot, so concurrent reads
// from BroadcastStatus never observe a partially-written struct.
var broadcastJobs sync.Map

// BroadcastProgress is a point-in-time snapshot of one broadcast job's delivery progress.
type BroadcastProgress struct {
	JobID       string
	Segment     string
	Total       int
	Sent        int
	Failed      int
	Unreachable int
	OtherFailed int
	Done        bool
	StartedAt   time.Time
	FinishedAt  *time.Time
}

// audienceFilter returns the predicate matching bot's existing broadcast segments
// (AdminBroadcastConfirm*Callback), so segment semantics stay identical between bot and web.
func audienceFilter(segment string) (func(database.Customer, time.Time) bool, error) {
	switch segment {
	case "active":
		return func(c database.Customer, now time.Time) bool { return c.ExpireAt != nil && !c.ExpireAt.Before(now) }, nil
	case "expired":
		return func(c database.Customer, now time.Time) bool { return c.ExpireAt != nil && c.ExpireAt.Before(now) }, nil
	case "inactive":
		return func(c database.Customer, now time.Time) bool { return c.ExpireAt == nil || c.ExpireAt.Before(now) }, nil
	case "new":
		return func(c database.Customer, now time.Time) bool { return c.ExpireAt == nil }, nil
	case "all":
		return func(database.Customer, time.Time) bool { return true }, nil
	default:
		return nil, fmt.Errorf("unknown broadcast segment %q", segment)
	}
}

// PreviewBroadcast returns how many customers match a segment, without sending anything. Used to
// show a recipient count before the admin confirms the send.
func (s *Service) PreviewBroadcast(ctx context.Context, segment string) (int, error) {
	filter, err := audienceFilter(segment)
	if err != nil {
		return 0, err
	}
	customers, err := s.customerRepository.FindAll(ctx)
	if err != nil {
		return 0, fmt.Errorf("load customers: %w", err)
	}
	now := time.Now()
	count := 0
	for _, c := range customers {
		if filter(c, now) {
			count++
		}
	}
	return count, nil
}

// RunBroadcast resolves the target audience and starts delivery in the background, returning a
// job ID the caller can poll via BroadcastStatus. Mirrors the bot's runBroadcast/deliverBroadcast
// exactly (same throttling, same unreachable-recipient classification).
func (s *Service) RunBroadcast(ctx context.Context, text, segment, source string) (string, error) {
	filter, err := audienceFilter(segment)
	if err != nil {
		return "", err
	}
	customers, err := s.customerRepository.FindAll(ctx)
	if err != nil {
		return "", fmt.Errorf("load customers: %w", err)
	}
	now := time.Now()
	recipients := make([]int64, 0, len(customers))
	for _, c := range customers {
		if filter(c, now) {
			recipients = append(recipients, c.TelegramID)
		}
	}

	jobID := uuid.NewString()
	broadcastJobs.Store(jobID, BroadcastProgress{
		JobID: jobID, Segment: segment, Total: len(recipients), StartedAt: time.Now(),
	})

	// Detach from the request context so the loop isn't cancelled once the HTTP/bot handler
	// returns (matches the bot's context.WithoutCancel usage in runBroadcast).
	bgCtx := context.WithoutCancel(ctx)
	go s.deliverBroadcast(bgCtx, jobID, text, segment, recipients, source)
	return jobID, nil
}

func (s *Service) deliverBroadcast(ctx context.Context, jobID, text, segment string, recipients []int64, source string) {
	startedAt := time.Now()
	sent, unreachable, otherFailed := 0, 0, 0
	total := len(recipients)

	snapshot := func(done bool) {
		p := BroadcastProgress{
			JobID: jobID, Segment: segment, Total: total,
			Sent: sent, Failed: unreachable + otherFailed, Unreachable: unreachable, OtherFailed: otherFailed,
			Done: done, StartedAt: startedAt,
		}
		if done {
			now := time.Now()
			p.FinishedAt = &now
		}
		broadcastJobs.Store(jobID, p)
	}

	for _, telegramID := range recipients {
		if s.telegramBot != nil {
			_, err := s.telegramBot.SendMessage(ctx, &bot.SendMessageParams{ChatID: telegramID, Text: text, ParseMode: models.ParseModeHTML})
			switch {
			case err == nil:
				sent++
			case isUserUnreachable(err):
				unreachable++
			default:
				otherFailed++
				slog.Warn("adminops broadcast: send failed", "telegram_id", telegramID, "error", err)
			}
		}
		snapshot(false)
		time.Sleep(40 * time.Millisecond)
	}
	snapshot(true)

	failed := unreachable + otherFailed
	slog.Info("adminops broadcast: done", "segment", segment, "sent", sent, "failed", failed, "unreachable", unreachable, "other", otherFailed)
	s.audit(ctx, "broadcast_"+segment, 0, intPtr(sent), nil, source)
}

// SendTestBroadcast sends the draft broadcast text only to the configured admin telegram ID, so
// the admin can preview formatting/content before committing to a real send. Synchronous (no job
// tracking needed for a single recipient) and always audit-logged, matching every other adminops
// mutation. Preserves the bot's old "🧪 Только мне" test-send capability, which had no equivalent
// in the web panel until now.
func (s *Service) SendTestBroadcast(ctx context.Context, text, source string) error {
	if text == "" {
		return fmt.Errorf("broadcast text is required")
	}
	adminID := config.GetAdminTelegramId()
	var sendErr error
	if s.telegramBot != nil {
		_, sendErr = s.telegramBot.SendMessage(ctx, &bot.SendMessageParams{
			ChatID: adminID, Text: text, ParseMode: models.ParseModeHTML,
		})
	}
	s.audit(ctx, "broadcast_test", adminID, nil, sendErr, source)
	return sendErr
}

// BroadcastStatus returns the latest known progress for a broadcast job. ok is false when the job
// ID is unknown (never started, or the process restarted since).
func (s *Service) BroadcastStatus(jobID string) (BroadcastProgress, bool) {
	v, ok := broadcastJobs.Load(jobID)
	if !ok {
		return BroadcastProgress{}, false
	}
	return v.(BroadcastProgress), true
}

// isUserUnreachable reports whether a SendMessage error means the recipient can't be reached
// (blocked the bot, deactivated their account, or never started a chat). Expected for cold
// audiences, not an actionable bug.
func isUserUnreachable(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "forbidden") ||
		strings.Contains(msg, "bot was blocked") ||
		strings.Contains(msg, "user is deactivated") ||
		strings.Contains(msg, "chat not found")
}
