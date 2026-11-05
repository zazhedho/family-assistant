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
	defer rows.Close()
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
