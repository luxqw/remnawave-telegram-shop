package handler

import (
	"testing"

	"github.com/go-telegram/bot/models"
)

func TestIsCandidateAdminReply(t *testing.T) {
	tests := []struct {
		name   string
		update *models.Update
		want   bool
	}{
		{
			name:   "nil message",
			update: &models.Update{},
			want:   false,
		},
		{
			name: "empty text",
			update: &models.Update{Message: &models.Message{
				Text: "", From: &models.User{ID: 42},
			}},
			want: false,
		},
		{
			name: "command prefix ignored",
			update: &models.Update{Message: &models.Message{
				Text: "/start", From: &models.User{ID: 42},
			}},
			want: false,
		},
		{
			name: "plain customer text is a candidate",
			update: &models.Update{Message: &models.Message{
				Text: "hey, question about my subscription", From: &models.User{ID: 42},
			}},
			want: true,
		},
		{
			// config.GetAdminTelegramId() is the zero value in this test binary (config.Load was
			// never called), so From.ID: 0 exercises the "message is from the admin" exclusion.
			name: "admin's own message excluded",
			update: &models.Update{Message: &models.Message{
				Text: "hey", From: &models.User{ID: 0},
			}},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := IsCandidateAdminReply(tt.update)
			if got != tt.want {
				t.Fatalf("want %v, got %v", tt.want, got)
			}
		})
	}
}
