package main

import (
	"context"
	"database/sql"
	"strings"
	"testing"
)

func TestQueryAiRunsIncludesProviderSchemaIdentity(t *testing.T) {
	t.Parallel()

	database, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if _, err := database.Exec(`
		CREATE TABLE ai_runs (
			id TEXT, job_id TEXT, outcome TEXT, error_code TEXT,
			provider_config_version INTEGER, provider_config_fingerprint TEXT,
			model TEXT, prompt_version TEXT, extraction_schema_version TEXT,
			provider_schema_version TEXT, provider_schema_sha256 TEXT,
			claim_schema_version TEXT, claim_mapper_version TEXT,
			input_processing_version TEXT, request_hash TEXT, response_hash TEXT,
			input_tokens INTEGER, output_tokens INTEGER, latency_ms INTEGER,
			started_at TEXT, finished_at TEXT
		);
		INSERT INTO ai_runs VALUES (
			'run', 'job', 'succeeded', NULL, 1, 'safe-fingerprint',
			'model', 'bill-visible-text-cn/1', 'bill-visible-text/1',
			'bill-visible-text-provider/1', '` + strings.Repeat("c", 64) + `',
			'document-claim/2', 'claim-mapper/3',
			'document-normalize/1', 'request', 'response', 10, 20, 30,
			'2026-08-28T00:00:00Z', '2026-08-28T00:00:01Z'
		)
	`); err != nil {
		t.Fatal(err)
	}
	runs, err := queryAiRuns(context.Background(), database, "job")
	if err != nil {
		t.Fatal(err)
	}
	if len(runs) != 1 || runs[0].ProviderSchemaVersion != "bill-visible-text-provider/1" ||
		runs[0].ProviderSchemaSHA256 != strings.Repeat("c", 64) || runs[0].ClaimMapperVersion != "claim-mapper/3" {
		t.Fatalf("AI run report = %#v", runs)
	}
}
