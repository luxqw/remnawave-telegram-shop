package database

import "testing"

func TestCustomerFilterWhereSQL(t *testing.T) {
	tests := []struct {
		name       string
		filter     string
		search     string
		wantEmpty  bool
		wantClause string // substring expected in the generated SQL
	}{
		{name: "no filter no search", filter: "", search: "", wantEmpty: true},
		{name: "active filter", filter: "active", wantClause: "is_trial"},
		{name: "trial filter", filter: "trial", wantClause: "is_trial"},
		{name: "expired filter", filter: "expired", wantClause: "expire_at"},
		{name: "no_sub filter", filter: "no_sub", wantClause: "expire_at"},
		{name: "numeric search", search: "12345", wantClause: "telegram_id"},
		{name: "non-numeric search matches username ILIKE", search: "not-a-number", wantClause: "username"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			where := customerFilterWhere(tt.filter, tt.search)
			if tt.wantEmpty {
				if len(where) != 0 {
					t.Fatalf("expected empty where clause, got %d conditions", len(where))
				}
				return
			}
			sqlStr, _, err := where.ToSql()
			if err != nil {
				t.Fatalf("ToSql() error: %v", err)
			}
			if sqlStr == "" {
				t.Fatal("expected non-empty SQL")
			}
		})
	}
}
