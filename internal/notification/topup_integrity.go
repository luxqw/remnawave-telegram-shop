package notification

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"time"

	"remnawave-tg-shop-bot/internal/database"
	"remnawave-tg-shop-bot/internal/remnawave"
)

type topupIntegrityRepository interface {
	FindAllLatestCompletedPerUser(ctx context.Context) ([]*database.TrafficTopup, error)
}

type TopupIntegrityService struct {
	topupRepository topupIntegrityRepository
	remnawaveClient *remnawave.Client
}

func NewTopupIntegrityService(topupRepository topupIntegrityRepository, remnawaveClient *remnawave.Client) *TopupIntegrityService {
	return &TopupIntegrityService{
		topupRepository: topupRepository,
		remnawaveClient: remnawaveClient,
	}
}

// CheckAndReapply fetches all users with completed topups and re-applies any that were
// lost due to a Remnawave periodic traffic reset restoring the base plan limit.
func (s *TopupIntegrityService) CheckAndReapply(ctx context.Context) error {
	topups, err := s.topupRepository.FindAllLatestCompletedPerUser(ctx)
	if err != nil {
		return fmt.Errorf("topup integrity: %w", err)
	}
	if len(topups) == 0 {
		return nil
	}

	restored, failures := 0, 0
	for _, topup := range topups {
		if ctx.Err() != nil {
			break
		}
		if topup.TargetTrafficLimitBytes == nil || topup.RemnawaveUUID == "" {
			continue
		}

		// Guard against int64 → int overflow on 32-bit platforms.
		if *topup.TargetTrafficLimitBytes > math.MaxInt {
			slog.Error("topup integrity: target overflows int, skipping",
				"telegram_id", topup.TelegramID,
				"target_bytes", *topup.TargetTrafficLimitBytes,
			)
			failures++
			continue
		}

		userCtx, userCancel := context.WithTimeout(ctx, 10*time.Second)
		users, err := s.remnawaveClient.GetUsersByTelegramID(userCtx, topup.TelegramID)
		userCancel()
		if err != nil || len(users) == 0 {
			slog.Warn("topup integrity: skip user", "telegram_id", topup.TelegramID, "error", err)
			continue
		}

		user := users[0]

		// Only restore for active users — do not re-enable disabled or expired accounts.
		if user.Status != "ACTIVE" {
			continue
		}

		if int64(user.TrafficLimitBytes) >= *topup.TargetTrafficLimitBytes {
			continue
		}

		target := int(*topup.TargetTrafficLimitBytes)
		applyCtx, applyCancel := context.WithTimeout(ctx, 10*time.Second)
		applyErr := s.remnawaveClient.UpdateUserTrafficLimit(applyCtx, user.UUID, target, user.TrafficLimitStrategy)
		applyCancel()
		if applyErr != nil {
			slog.Error("topup integrity: re-apply failed",
				"telegram_id", topup.TelegramID,
				"current_bytes", user.TrafficLimitBytes,
				"target_bytes", target,
				"error", applyErr,
			)
			failures++
			continue
		}

		slog.Info("topup integrity: restored topup after reset",
			"telegram_id", topup.TelegramID,
			"was_bytes", user.TrafficLimitBytes,
			"restored_bytes", target,
		)
		restored++
	}

	slog.Info("topup integrity: done", "restored", restored, "failures", failures, "checked", len(topups))
	return nil
}
