//go:build !local_engine && !nosqlite

package storage

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"
)

// TestSQLiteModelPricingMigratesContextWindow verifies that a DB created with
// an older schema (model_pricing without the context_window column) is migrated
// on startup so seeding/upserting pricing no longer fails with "table model_pricing
// has no column named context_window".
func TestSQLiteModelPricingMigratesContextWindow(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "labubu.db")

	// Create an old-style model_pricing table lacking context_window.
	raw, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open raw sqlite: %v", err)
	}
	if _, err := raw.Exec(`CREATE TABLE model_pricing (
		model_name  TEXT NOT NULL PRIMARY KEY,
		input_price REAL NOT NULL,
		output_price REAL NOT NULL,
		currency    TEXT NOT NULL DEFAULT 'USD'
	)`); err != nil {
		t.Fatalf("create old table: %v", err)
	}
	if _, err := raw.Exec(`INSERT INTO model_pricing (model_name, input_price, output_price, currency)
		VALUES ('legacy-model', 1.0, 2.0, 'USD')`); err != nil {
		t.Fatalf("seed old row: %v", err)
	}
	if err := raw.Close(); err != nil {
		t.Fatalf("close raw: %v", err)
	}

	// Opening the store runs migrations + default pricing seed.
	store, err := NewChDBStore(dir)
	if err != nil {
		t.Fatalf("NewChDBStore on old DB: %v", err)
	}
	defer store.Close()
	ctx := context.Background()

	// Upserting must not error on the migrated column.
	if err := store.UpsertModelPricing(ctx, ModelPricing{
		ModelName: "glm-5.2", InputPrice: 10.0, OutputPrice: 30.0,
		Currency: "CNY", ContextWindow: 128000,
	}); err != nil {
		t.Fatalf("UpsertModelPricing after migration: %v", err)
	}

	// The legacy row survives; the new row carries context_window.
	pricings, err := store.GetModelPricing(ctx)
	if err != nil {
		t.Fatalf("GetModelPricing: %v", err)
	}
	byName := map[string]ModelPricing{}
	for _, p := range pricings {
		byName[p.ModelName] = p
	}
	if p, ok := byName["legacy-model"]; !ok {
		t.Errorf("legacy-model row lost after migration")
	} else if p.ContextWindow != 0 {
		t.Errorf("legacy-model context_window = %d, want 0 (default)", p.ContextWindow)
	}
	if p, ok := byName["glm-5.2"]; !ok {
		t.Errorf("glm-5.2 not found after upsert")
	} else if p.ContextWindow != 128000 {
		t.Errorf("glm-5.2 context_window = %d, want 128000", p.ContextWindow)
	}
}
