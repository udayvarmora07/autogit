package db

import "testing"

func FuzzMigrationBoundaryNeverPanics(f *testing.F) {
	f.Add("7", "3.51.3", "3.53.4")
	f.Add("not-a-version", "", "3.51.3")
	f.Fuzz(func(t *testing.T, schema, gotSQLite, wantSQLite string) {
		_, _ = parseSchemaVersion(schema)
		_ = atLeastVersion(gotSQLite, wantSQLite)
	})
}
