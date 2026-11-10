package main

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
)

const activityColumnsQuery = `SELECT column_name FROM information_schema.columns WHERE table_schema = current_schema() AND table_name = 'space_activities' AND column_name IN ('details', 'subject')`

func TestCheckActivityColumnsLogsMissingColumns(t *testing.T) {
	for _, tc := range []struct {
		name    string
		present []string
		missing []string
	}{
		{name: "both present", present: []string{"details", "subject"}},
		{name: "subject missing", present: []string{"details"}, missing: []string{"subject"}},
		{name: "both missing", missing: []string{"details", "subject"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherEqual))
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = db.Close() }()
			rows := sqlmock.NewRows([]string{"column_name"})
			for _, c := range tc.present {
				rows.AddRow(c)
			}
			mock.ExpectQuery(activityColumnsQuery).WillReturnRows(rows)
			var buf bytes.Buffer
			got := checkActivityColumns(context.Background(), db, slog.New(slog.NewTextHandler(&buf, nil)))
			if strings.Join(got, ",") != strings.Join(tc.missing, ",") {
				t.Fatalf("missing = %v, want %v", got, tc.missing)
			}
			out := buf.String()
			if len(tc.missing) == 0 {
				if out != "" {
					t.Fatalf("unexpected log: %s", out)
				}
			} else {
				if !strings.Contains(out, "level=ERROR") || !strings.Contains(out, "ALTER") {
					t.Fatalf("log = %s, want an ERROR with the ALTER hint", out)
				}
				for _, c := range tc.missing {
					if !strings.Contains(out, "space_activities."+c) {
						t.Fatalf("log = %s, want column %s named", out, c)
					}
				}
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestCheckActivityColumnsQueryFailureDoesNotStopStartup(t *testing.T) {
	db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherEqual))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	mock.ExpectQuery(activityColumnsQuery).WillReturnError(errors.New("boom"))
	var buf bytes.Buffer
	if got := checkActivityColumns(context.Background(), db, slog.New(slog.NewTextHandler(&buf, nil))); got != nil {
		t.Fatalf("missing = %v, want nil when the check cannot run", got)
	}
	if !strings.Contains(buf.String(), "level=ERROR") {
		t.Fatalf("log = %s", buf.String())
	}
}

const financeTablesQuery = `SELECT table_name FROM information_schema.tables WHERE table_schema = current_schema() AND table_name IN ('finance_accounts', 'finance_categories', 'finance_transactions', 'finance_budgets', 'finance_recurring')`

func TestSchemaCheckFinanceTablesLogsMissingTables(t *testing.T) {
	for _, tc := range []struct {
		name    string
		present []string
		missing []string
	}{
		{name: "all present", present: requiredFinanceTables},
		{name: "one missing", present: []string{"finance_accounts", "finance_categories", "finance_transactions", "finance_budgets"}, missing: []string{"finance_recurring"}},
		{name: "all missing", missing: requiredFinanceTables},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherEqual))
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = db.Close() }()
			rows := sqlmock.NewRows([]string{"table_name"})
			for _, name := range tc.present {
				rows.AddRow(name)
			}
			mock.ExpectQuery(financeTablesQuery).WillReturnRows(rows)
			var buf bytes.Buffer
			got := checkFinanceTables(context.Background(), db, slog.New(slog.NewTextHandler(&buf, nil)))
			if strings.Join(got, ",") != strings.Join(tc.missing, ",") {
				t.Fatalf("missing = %v, want %v", got, tc.missing)
			}
			out := buf.String()
			if len(tc.missing) == 0 {
				if out != "" {
					t.Fatalf("unexpected log: %s", out)
				}
			} else {
				if !strings.Contains(out, "level=ERROR") || !strings.Contains(out, "000013") {
					t.Fatalf("log = %s, want an ERROR naming migration 000013", out)
				}
				for _, name := range tc.missing {
					if !strings.Contains(out, name) {
						t.Fatalf("log = %s, want table %s named", out, name)
					}
				}
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestSchemaCheckFinanceTablesQueryFailureDoesNotStopStartup(t *testing.T) {
	db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherEqual))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	mock.ExpectQuery(financeTablesQuery).WillReturnError(errors.New("boom"))
	var buf bytes.Buffer
	if got := checkFinanceTables(context.Background(), db, slog.New(slog.NewTextHandler(&buf, nil))); got != nil {
		t.Fatalf("missing = %v, want nil when the check cannot run", got)
	}
	if !strings.Contains(buf.String(), "level=ERROR") {
		t.Fatalf("log = %s", buf.String())
	}
}
