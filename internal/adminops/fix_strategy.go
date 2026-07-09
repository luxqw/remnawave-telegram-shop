package adminops

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"

	"remnawave-tg-shop-bot/internal/config"
)

// ErrFixStrategyInProgress is returned by FixStrategyApply when a bulk apply run is already in
// flight (mirrors the bot's fixStrategyMu.TryLock() guard in admin_fix_strategy.go).
var ErrFixStrategyInProgress = errors.New("fix-traffic-strategy apply is already running")

var fixStrategyMu sync.Mutex

// FixStrategyPreviewResult summarizes how many customers are on each Remnawave traffic-limit
// reset strategy vs. the configured target, without changing anything.
type FixStrategyPreviewResult struct {
	TotalCustomers int
	StrategyCounts map[string]int
	NotFound       int
	TargetStrategy string
	WillUpdate     int
}

// FixStrategyPreview mirrors the bot's fixStrategyPreview exactly. Read-only, so it is not
// audit-logged (matches the plan: only the bulk apply is a mutation worth auditing).
func (s *Service) FixStrategyPreview(ctx context.Context) (FixStrategyPreviewResult, error) {
	customers, err := s.customerRepository.FindAll(ctx)
	if err != nil {
		return FixStrategyPreviewResult{}, fmt.Errorf("load customers: %w", err)
	}
	rwUsers, err := s.remnawaveClient.GetUsers(ctx)
	if err != nil {
		return FixStrategyPreviewResult{}, fmt.Errorf("load remnawave users: %w", err)
	}

	rwByTG := make(map[int64]string, len(rwUsers))
	for _, u := range rwUsers {
		if u.TelegramID != nil {
			rwByTG[*u.TelegramID] = u.TrafficLimitStrategy
		}
	}

	target := config.TrafficLimitResetStrategy()
	strategyCounts := make(map[string]int)
	notFound, willUpdate := 0, 0

	for _, c := range customers {
		strategy, found := rwByTG[c.TelegramID]
		if !found {
			notFound++
			continue
		}
		strategyCounts[strategy]++
		if strategy != target {
			willUpdate++
		}
	}

	return FixStrategyPreviewResult{
		TotalCustomers: len(customers),
		StrategyCounts: strategyCounts,
		NotFound:       notFound,
		TargetStrategy: target,
		WillUpdate:     willUpdate,
	}, nil
}

type rwEntry struct {
	uuid     uuid.UUID
	strategy string
}

// FixStrategyResult summarizes a completed (or partially completed, on error) bulk apply run.
type FixStrategyResult struct {
	Total    int
	Updated  int
	Skipped  int
	NotFound int
	Errors   []string
}

// FixStrategyApply bulk-updates every customer's Remnawave traffic-limit reset strategy to the
// configured target. Mirrors the bot's fixStrategyApply exactly (same throttling, same guard
// against concurrent runs), except it runs synchronously in the caller's goroutine and reports
// progress via the callback instead of sending Telegram messages directly — callers (bot adapter,
// web handler) decide how to surface that progress. Always audit-logs the run (new: the bot
// version never wrote this to admin_audit_log).
func (s *Service) FixStrategyApply(ctx context.Context, source string, progress func(processed, total, updated, errored int)) (FixStrategyResult, error) {
	if !fixStrategyMu.TryLock() {
		return FixStrategyResult{}, ErrFixStrategyInProgress
	}
	defer fixStrategyMu.Unlock()

	target := config.TrafficLimitResetStrategy()

	customers, err := s.customerRepository.FindAll(ctx)
	if err != nil {
		s.audit(ctx, "fix_strategy_apply", 0, nil, err, source)
		return FixStrategyResult{}, fmt.Errorf("load customers: %w", err)
	}
	rwUsers, err := s.remnawaveClient.GetUsers(ctx)
	if err != nil {
		s.audit(ctx, "fix_strategy_apply", 0, nil, err, source)
		return FixStrategyResult{}, fmt.Errorf("load remnawave users: %w", err)
	}

	rwByTG := make(map[int64]rwEntry, len(rwUsers))
	for _, u := range rwUsers {
		if u.TelegramID != nil {
			rwByTG[*u.TelegramID] = rwEntry{uuid: u.UUID, strategy: u.TrafficLimitStrategy}
		}
	}

	total := len(customers)
	updated, skipped, notFound := 0, 0, 0
	var errUUIDs []string

	for i, c := range customers {
		entry, found := rwByTG[c.TelegramID]
		switch {
		case !found:
			notFound++
		case entry.strategy == target:
			skipped++
		default:
			reqCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
			updateErr := s.remnawaveClient.UpdateUserStrategy(reqCtx, entry.uuid, target)
			cancel()
			if updateErr != nil {
				slog.Error("adminops fix_strategy apply: update failed", "uuid", entry.uuid, "error", updateErr)
				errUUIDs = append(errUUIDs, entry.uuid.String())
			} else {
				updated++
			}
			time.Sleep(100 * time.Millisecond)
		}
		if progress != nil {
			progress(i+1, total, updated, len(errUUIDs))
		}
	}

	result := FixStrategyResult{Total: total, Updated: updated, Skipped: skipped, NotFound: notFound, Errors: errUUIDs}

	var auditErr error
	if len(errUUIDs) > 0 {
		auditErr = fmt.Errorf("%d remnawave update errors during bulk strategy apply", len(errUUIDs))
	}
	s.audit(ctx, "fix_strategy_apply", 0, intPtr(updated), auditErr, source)
	return result, nil
}
