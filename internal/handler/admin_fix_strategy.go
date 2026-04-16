package handler

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
)

var fixStrategyMu sync.Mutex

func (h Handler) FixTrafficStrategyCommandHandler(ctx context.Context, b *bot.Bot, update *models.Update) {
	text := strings.TrimSpace(update.Message.Text)
	chatID := update.Message.Chat.ID

	parts := strings.Fields(text)
	if len(parts) < 2 {
		_, _ = b.SendMessage(ctx, &bot.SendMessageParams{
			ChatID: chatID,
			Text:   "Использование:\n/fix_traffic_strategy preview\n/fix_traffic_strategy apply",
		})
		return
	}

	switch parts[1] {
	case "preview":
		h.fixStrategyPreview(ctx, b, chatID)
	case "apply":
		h.fixStrategyApply(ctx, b, chatID)
	default:
		_, _ = b.SendMessage(ctx, &bot.SendMessageParams{
			ChatID: chatID,
			Text:   "Неизвестная подкоманда. Используй: preview или apply",
		})
	}
}

func (h Handler) fixStrategyPreview(ctx context.Context, b *bot.Bot, chatID int64) {
	customers, err := h.customerRepository.FindAll(ctx)
	if err != nil {
		slog.Error("fix_strategy preview: failed to load customers", "error", err)
		_, _ = b.SendMessage(ctx, &bot.SendMessageParams{ChatID: chatID, Text: "Ошибка загрузки юзеров из БД: " + err.Error()})
		return
	}

	rwUsers, err := h.remnawaveClient.GetUsers(ctx)
	if err != nil {
		slog.Error("fix_strategy preview: failed to load remnawave users", "error", err)
		_, _ = b.SendMessage(ctx, &bot.SendMessageParams{ChatID: chatID, Text: "Ошибка загрузки юзеров из Remnawave: " + err.Error()})
		return
	}

	rwByTG := make(map[int64]string, len(rwUsers))
	for _, u := range rwUsers {
		if u.TelegramID != nil {
			rwByTG[*u.TelegramID] = u.TrafficLimitStrategy
		}
	}

	target := config.TrafficLimitResetStrategy()
	strategyCounts := make(map[string]int)
	notFound := 0
	willUpdate := 0

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

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Всего юзеров в БД: %d\n", len(customers)))
	sb.WriteString("Из них в Remnawave:\n")
	for _, s := range []string{"MONTH_ROLLING", "MONTH", "NO_RESET", "DAY", "WEEK"} {
		if n, ok := strategyCounts[s]; ok {
			sb.WriteString(fmt.Sprintf("  %s: %d\n", s, n))
		}
	}
	for s, n := range strategyCounts {
		switch s {
		case "MONTH_ROLLING", "MONTH", "NO_RESET", "DAY", "WEEK":
		default:
			sb.WriteString(fmt.Sprintf("  %s (другое): %d\n", s, n))
		}
	}
	sb.WriteString(fmt.Sprintf("  не найдено в панели: %d\n", notFound))
	sb.WriteString(fmt.Sprintf("\nЦелевая стратегия: %s\n", target))
	sb.WriteString(fmt.Sprintf("Будет обновлено: %d", willUpdate))

	_, _ = b.SendMessage(ctx, &bot.SendMessageParams{ChatID: chatID, Text: sb.String()})
}

type rwEntry struct {
	uuid     uuid.UUID
	strategy string
}

func (h Handler) fixStrategyApply(ctx context.Context, b *bot.Bot, chatID int64) {
	if !fixStrategyMu.TryLock() {
		_, _ = b.SendMessage(ctx, &bot.SendMessageParams{ChatID: chatID, Text: "Уже идёт применение стратегии. Подожди завершения."})
		return
	}

	_, _ = b.SendMessage(ctx, &bot.SendMessageParams{ChatID: chatID, Text: "Запустил в фоне. Пришлю отчёт о прогрессе."})

	go func() {
		defer fixStrategyMu.Unlock()

		bgCtx := context.Background()
		target := config.TrafficLimitResetStrategy()

		customers, err := h.customerRepository.FindAll(bgCtx)
		if err != nil {
			slog.Error("fix_strategy apply: failed to load customers", "error", err)
			_, _ = b.SendMessage(bgCtx, &bot.SendMessageParams{ChatID: chatID, Text: "Ошибка загрузки юзеров из БД: " + err.Error()})
			return
		}

		rwUsers, err := h.remnawaveClient.GetUsers(bgCtx)
		if err != nil {
			slog.Error("fix_strategy apply: failed to load remnawave users", "error", err)
			_, _ = b.SendMessage(bgCtx, &bot.SendMessageParams{ChatID: chatID, Text: "Ошибка загрузки юзеров из Remnawave: " + err.Error()})
			return
		}

		rwByTG := make(map[int64]rwEntry, len(rwUsers))
		for _, u := range rwUsers {
			if u.TelegramID != nil {
				rwByTG[*u.TelegramID] = rwEntry{uuid: u.UUID, strategy: u.TrafficLimitStrategy}
			}
		}

		total := len(customers)
		updated := 0
		skipped := 0
		notFound := 0
		var errorUUIDs []string

		for i, c := range customers {
			entry, found := rwByTG[c.TelegramID]
			if !found {
				notFound++
				continue
			}
			if entry.strategy == target {
				skipped++
				continue
			}

			reqCtx, cancel := context.WithTimeout(bgCtx, 10*time.Second)
			err := h.remnawaveClient.UpdateUserStrategy(reqCtx, entry.uuid, target)
			cancel()

			if err != nil {
				slog.Error("fix_strategy apply: update failed", "uuid", entry.uuid, "error", err)
				errorUUIDs = append(errorUUIDs, entry.uuid.String())
			} else {
				updated++
			}

			time.Sleep(100 * time.Millisecond)

			processed := i + 1
			if processed%50 == 0 {
				msg := fmt.Sprintf("Обработано %d/%d, обновлено %d, ошибок %d", processed, total, updated, len(errorUUIDs))
				_, _ = b.SendMessage(bgCtx, &bot.SendMessageParams{ChatID: chatID, Text: msg})
			}
		}

		var sb strings.Builder
		sb.WriteString(fmt.Sprintf(
			"Готово!\nВсего юзеров в БД: %d\nОбновлено: %d\nПропущено (уже верная стратегия): %d\nНе найдено в панели: %d\nОшибок: %d",
			total, updated, skipped, notFound, len(errorUUIDs),
		))
		if len(errorUUIDs) > 0 {
			sb.WriteString("\n\nUUID с ошибками:")
			for _, u := range errorUUIDs {
				sb.WriteString("\n- " + u)
			}
		}
		_, _ = b.SendMessage(bgCtx, &bot.SendMessageParams{ChatID: chatID, Text: sb.String()})
	}()
}
