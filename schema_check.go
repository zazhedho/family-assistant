package main

import (
	"context"
	"database/sql"
	"log/slog"
)

// requiredActivityColumns are added to existing databases by a manual ALTER
// (migration 000011 only creates them on new databases).
var requiredActivityColumns = []string{"details", "subject"}

// checkActivityColumns logs loudly when the space_activities columns added for
// structured details are missing, and returns their names. It never stops
// startup: reads and reminders keep working without them.
func checkActivityColumns(ctx context.Context, db *sql.DB, logger *slog.Logger) []string {
	rows, err := db.QueryContext(ctx, `SELECT column_name FROM information_schema.columns WHERE table_schema = current_schema() AND table_name = 'space_activities' AND column_name IN ('details', 'subject')`)
	if err != nil {
		logger.Error("schema check: cannot read space_activities columns", "error", err)
		return nil
	}
	defer func() { _ = rows.Close() }()
	present := map[string]bool{}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			logger.Error("schema check: cannot read space_activities columns", "error", err)
			return nil
		}
		present[name] = true
	}
	if err := rows.Err(); err != nil {
		logger.Error("schema check: cannot read space_activities columns", "error", err)
		return nil
	}
	var missing []string
	for _, column := range requiredActivityColumns {
		if !present[column] {
			missing = append(missing, column)
		}
	}
	if len(missing) == 0 {
		return nil
	}
	qualified := make([]string, len(missing))
	for i, column := range missing {
		qualified[i] = "space_activities." + column
	}
	logger.Error("SCHEMA OUT OF DATE: structured activity columns are missing; apply the manual ALTER TABLE space_activities (ADD COLUMN details JSONB NOT NULL DEFAULT '{}', ADD COLUMN subject VARCHAR(128) and their constraints) before using details or feeding reports",
		"missing_columns", qualified)
	return missing
}

// requiredFinanceTables are created by migration 000013. Existing databases
// need that migration applied by hand before the finance tools can be used.
var requiredFinanceTables = []string{"finance_accounts", "finance_categories", "finance_transactions", "finance_budgets", "finance_recurring"}

// checkFinanceTables logs loudly when any finance table is missing and returns
// the missing names. It never stops startup: the other tools keep working.
func checkFinanceTables(ctx context.Context, db *sql.DB, logger *slog.Logger) []string {
	rows, err := db.QueryContext(ctx, `SELECT table_name FROM information_schema.tables WHERE table_schema = current_schema() AND table_name IN ('finance_accounts', 'finance_categories', 'finance_transactions', 'finance_budgets', 'finance_recurring')`)
	if err != nil {
		logger.Error("schema check: cannot read finance tables", "error", err)
		return nil
	}
	defer func() { _ = rows.Close() }()
	present := map[string]bool{}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			logger.Error("schema check: cannot read finance tables", "error", err)
			return nil
		}
		present[name] = true
	}
	if err := rows.Err(); err != nil {
		logger.Error("schema check: cannot read finance tables", "error", err)
		return nil
	}
	var missing []string
	for _, table := range requiredFinanceTables {
		if !present[table] {
			missing = append(missing, table)
		}
	}
	if len(missing) == 0 {
		return nil
	}
	logger.Error("SCHEMA OUT OF DATE: finance tables are missing; apply migration 000013_create_finance_tables before using finance tools",
		"missing_tables", missing)
	return missing
}

// financeEnabled reports whether the finance tools and the recurring scheduler
// may start: only when checkFinanceTables found no missing table. An
// unreadable catalog reports nothing missing, so that case keeps finance on
// (the previous behavior) rather than disabling it on a transient error.
func financeEnabled(missing []string) bool { return len(missing) == 0 }
