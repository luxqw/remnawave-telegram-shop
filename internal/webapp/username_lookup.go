package webapp

import (
	"context"
	"log/slog"
)

// usernamesByTelegramID batch-resolves telegram_id -> username (nil when unknown or unset) for
// hydrating list/detail DTOs that only carry a telegram_id, so the admin UI can build direct
// t.me/<username> links instead of just showing the bare ID.
func (h *Handler) usernamesByTelegramID(ctx context.Context, ids []int64) map[int64]*string {
	if len(ids) == 0 {
		return nil
	}
	seen := make(map[int64]bool, len(ids))
	unique := make([]int64, 0, len(ids))
	for _, id := range ids {
		if !seen[id] {
			seen[id] = true
			unique = append(unique, id)
		}
	}
	customers, err := h.customerRepository.FindByTelegramIds(ctx, unique)
	if err != nil {
		slog.Warn("failed to resolve usernames", "error", err)
		return nil
	}
	out := make(map[int64]*string, len(customers))
	for _, c := range customers {
		out[c.TelegramID] = c.Username
	}
	return out
}
