package handler

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"

	"remnawave-tg-shop-bot/internal/adminops"
)

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

// fixStrategyPreview delegates to adminops.Service.FixStrategyPreview and formats the same report
// the bot has always sent.
func (h Handler) fixStrategyPreview(ctx context.Context, b *bot.Bot, chatID int64) {
	result, err := h.adminOps.FixStrategyPreview(ctx)
	if err != nil {
		_, _ = b.SendMessage(ctx, &bot.SendMessageParams{ChatID: chatID, Text: "Ошибка: " + err.Error()})
		return
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Всего юзеров в БД: %d\n", result.TotalCustomers))
	sb.WriteString("Из них в Remnawave:\n")
	for _, s := range []string{"MONTH_ROLLING", "MONTH", "NO_RESET", "DAY", "WEEK"} {
		if n, ok := result.StrategyCounts[s]; ok {
			sb.WriteString(fmt.Sprintf("  %s: %d\n", s, n))
		}
	}
	for s, n := range result.StrategyCounts {
		switch s {
		case "MONTH_ROLLING", "MONTH", "NO_RESET", "DAY", "WEEK":
		default:
			sb.WriteString(fmt.Sprintf("  %s (другое): %d\n", s, n))
		}
	}
	sb.WriteString(fmt.Sprintf("  не найдено в панели: %d\n", result.NotFound))
	sb.WriteString(fmt.Sprintf("\nЦелевая стратегия: %s\n", result.TargetStrategy))
	sb.WriteString(fmt.Sprintf("Будет обновлено: %d", result.WillUpdate))

	_, _ = b.SendMessage(ctx, &bot.SendMessageParams{ChatID: chatID, Text: sb.String()})
}

// fixStrategyApply delegates to adminops.Service.FixStrategyApply in the background, reporting
// progress every 50 processed customers and a final summary — matching the bot's original
// behavior exactly, except the guard-against-concurrent-runs lock and the mutation itself now
// live in adminops (shared with the web API) and the run is unconditionally audit-logged.
func (h Handler) fixStrategyApply(ctx context.Context, b *bot.Bot, chatID int64) {
	_, _ = b.SendMessage(ctx, &bot.SendMessageParams{ChatID: chatID, Text: "Запустил в фоне. Пришлю отчёт о прогрессе."})

	bgCtx := context.WithoutCancel(ctx)
	go func() {
		result, err := h.adminOps.FixStrategyApply(bgCtx, "command", func(processed, total, updated, errored int) {
			if processed%50 == 0 {
				msg := fmt.Sprintf("Обработано %d/%d, обновлено %d, ошибок %d", processed, total, updated, errored)
				_, _ = b.SendMessage(bgCtx, &bot.SendMessageParams{ChatID: chatID, Text: msg})
			}
		})
		if err != nil {
			if errors.Is(err, adminops.ErrFixStrategyInProgress) {
				_, _ = b.SendMessage(bgCtx, &bot.SendMessageParams{ChatID: chatID, Text: "Уже идёт применение стратегии. Подожди завершения."})
				return
			}
			_, _ = b.SendMessage(bgCtx, &bot.SendMessageParams{ChatID: chatID, Text: "Ошибка: " + err.Error()})
			return
		}

		var sb strings.Builder
		sb.WriteString(fmt.Sprintf(
			"Готово!\nВсего юзеров в БД: %d\nОбновлено: %d\nПропущено (уже верная стратегия): %d\nНе найдено в панели: %d\nОшибок: %d",
			result.Total, result.Updated, result.Skipped, result.NotFound, len(result.Errors),
		))
		if len(result.Errors) > 0 {
			sb.WriteString("\n\nUUID с ошибками:")
			for _, u := range result.Errors {
				sb.WriteString("\n- " + u)
			}
		}
		_, _ = b.SendMessage(bgCtx, &bot.SendMessageParams{ChatID: chatID, Text: sb.String()})
	}()
}
