package main

import (
	"context"
	"strings"
	"testing"

	"github.com/tuoro/smart-bill-manager/apps/api/internal/testsupport/postgresqltest"
)

func TestQueryAiRunsIncludesProviderSchemaIdentity(t *testing.T) {
	database := postgresqltest.OpenEmptyDatabase(t)
	if _, err := database.Exec(`
		CREATE TABLE ai_runs (
			id TEXT, job_id TEXT, outcome TEXT, error_code TEXT,
			provider_config_version BIGINT, provider_config_fingerprint TEXT,
			model TEXT, prompt_version TEXT, extraction_schema_version TEXT,
			provider_schema_version TEXT, provider_schema_sha256 TEXT,
			claim_schema_version TEXT, claim_mapper_version TEXT,
			input_processing_version TEXT, request_hash TEXT, response_hash TEXT,
			input_tokens BIGINT, output_tokens BIGINT, latency_ms BIGINT,
			started_at TIMESTAMPTZ, finished_at TIMESTAMPTZ
		);
		INSERT INTO ai_runs VALUES (
			'run', 'job', 'succeeded', NULL, 1, 'safe-fingerprint',
			'model', 'bill-visible-text-cn/2', 'bill-visible-text/2',
			'bill-visible-text-provider/2', '` + strings.Repeat("c", 64) + `',
			'document-claim/3', 'claim-mapper/4',
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
	if len(runs) != 1 || runs[0].ProviderSchemaVersion != "bill-visible-text-provider/2" ||
		runs[0].ProviderSchemaSHA256 != strings.Repeat("c", 64) || runs[0].ClaimMapperVersion != "claim-mapper/4" {
		t.Fatalf("AI run report = %#v", runs)
	}
}
