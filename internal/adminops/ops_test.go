package adminops

import "testing"

func TestPluralDays(t *testing.T) {
	tests := []struct {
		n    int
		want string
	}{
		{1, "день"},
		{21, "день"},
		{2, "дня"},
		{3, "дня"},
		{4, "дня"},
		{22, "дня"},
		{5, "дней"},
		{11, "дней"},
		{12, "дней"},
		{20, "дней"},
		{100, "дней"},
	}
	for _, tt := range tests {
		if got := pluralDays(tt.n); got != tt.want {
			t.Errorf("pluralDays(%d) = %q, want %q", tt.n, got, tt.want)
		}
	}
}
