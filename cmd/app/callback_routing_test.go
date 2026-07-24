package main

import (
	"context"
	"testing"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"

	"remnawave-tg-shop-bot/internal/handler"
)

// Regression test for the payment/payment_cancel routing collision: "payment" is registered
// with MatchTypePrefix and is a literal string prefix of "payment_cancel", so registering it
// before CallbackPaymentCancel makes the router swallow every cancel-button tap into the wrong
// handler (see cmd/app/main.go registration comment). Mirrors main()'s registration order.
func TestPaymentCancelRoutingDoesNotCollideWithPayment(t *testing.T) {
	var gotCancel, gotPayment bool

	b, err := bot.New("test-token", bot.WithSkipGetMe(), bot.WithNotAsyncHandlers())
	if err != nil {
		t.Fatalf("bot.New: %v", err)
	}

	b.RegisterHandler(bot.HandlerTypeCallbackQueryData, handler.CallbackPaymentCancel, bot.MatchTypePrefix,
		func(ctx context.Context, b *bot.Bot, update *models.Update) { gotCancel = true })
	b.RegisterHandler(bot.HandlerTypeCallbackQueryData, handler.CallbackPayment, bot.MatchTypePrefix,
		func(ctx context.Context, b *bot.Bot, update *models.Update) { gotPayment = true })

	b.ProcessUpdate(context.Background(), &models.Update{
		CallbackQuery: &models.CallbackQuery{Data: "payment_cancel?id=42"},
	})
	if !gotCancel || gotPayment {
		t.Fatalf("payment_cancel?id=42: expected cancel handler only, got cancel=%v payment=%v", gotCancel, gotPayment)
	}

	gotCancel, gotPayment = false, false
	b.ProcessUpdate(context.Background(), &models.Update{
		CallbackQuery: &models.CallbackQuery{Data: "payment?month=1&invoiceType=rollypay&amount=100"},
	})
	if gotCancel || !gotPayment {
		t.Fatalf("payment?month=1...: expected payment handler only, got cancel=%v payment=%v", gotCancel, gotPayment)
	}
}
