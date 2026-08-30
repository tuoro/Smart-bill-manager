package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"math"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"time"

	_ "modernc.org/sqlite"
)

type evaluationRun struct {
	RunID   string `json:"run_id"`
	Samples []struct {
		SampleID string `json:"sample_id"`
		JobID    string `json:"job_id"`
	} `json:"samples"`
}

type aiRunRecord struct {
	ID                        string `json:"id"`
	Outcome                   string `json:"outcome"`
	ErrorCode                 string `json:"error_code,omitempty"`
	ProviderConfigVersion     int    `json:"provider_config_version"`
	ProviderConfigFingerprint string `json:"provider_config_safe_fingerprint"`
	Model                     string `json:"model"`
	PromptVersion             string `json:"prompt_version"`
	ExtractionSchemaVersion   string `json:"extraction_schema_version"`
	ProviderSchemaVersion     string `json:"provider_schema_version"`
	ProviderSchemaSHA256      string `json:"provider_schema_sha256"`
	ClaimSchemaVersion        string `json:"claim_schema_version"`
	ClaimMapperVersion        string `json:"claim_mapper_version"`
	InputProcessingVersion    string `json:"input_processing_version"`
	RequestHash               string `json:"request_hash,omitempty"`
	ResponseHash              string `json:"response_hash,omitempty"`
	InputTokens               *int64 `json:"input_tokens,omitempty"`
	OutputTokens              *int64 `json:"output_tokens,omitempty"`
	LatencyMS                 *int64 `json:"latency_ms,omitempty"`
	StartedAt                 string `json:"started_at"`
	FinishedAt                string `json:"finished_at,omitempty"`
}

type sampleReport struct {
	SampleID string        `json:"sample_id"`
	JobID    string        `json:"job_id"`
	AiRuns   []aiRunRecord `json:"ai_runs"`
}

type latencySummary struct {
	Count int   `json:"count"`
	Min   int64 `json:"min"`
	P50   int64 `json:"p50"`
	P95   int64 `json:"p95"`
	Max   int64 `json:"max"`
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "evaluation-report:", err)
		os.Exit(1)
	}
}

func run() error {
	var databasePath, runResultPath, outputPath string
	flag.StringVar(&databasePath, "database", "", "read-only path to the evaluation SQLite database")
	flag.StringVar(&runResultPath, "run-result", "", "model evaluation run JSON")
	flag.StringVar(&outputPath, "output", "", "new output JSON path")
	flag.Parse()
	if flag.NArg() != 0 || databasePath == "" || runResultPath == "" || outputPath == "" {
		return errors.New("-database, -run-result, and -output are required; positional arguments are not allowed")
	}
	runContent, err := os.ReadFile(runResultPath)
	if err != nil {
		return fmt.Errorf("read evaluation run: %w", err)
	}
	var input evaluationRun
	if err := json.Unmarshal(runContent, &input); err != nil {
		return fmt.Errorf("decode evaluation run: %w", err)
	}
	if input.RunID == "" || len(input.Samples) != 100 {
		return errors.New("evaluation run must contain an ID and exactly 100 samples")
	}
	database, err := openReadOnlyDatabase(databasePath)
	if err != nil {
		return err
	}
	defer database.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	reports := make([]sampleReport, 0, len(input.Samples))
	latencies := make([]int64, 0)
	for _, sample := range input.Samples {
		report := sampleReport{SampleID: sample.SampleID, JobID: sample.JobID, AiRuns: []aiRunRecord{}}
		if sample.JobID != "" {
			report.AiRuns, err = queryAiRuns(ctx, database, sample.JobID)
			if err != nil {
				return fmt.Errorf("sample %s: %w", sample.SampleID, err)
			}
			for _, item := range report.AiRuns {
				if item.Outcome == "running" {
					return fmt.Errorf("sample %s has a non-terminal AI run", sample.SampleID)
				}
				if item.LatencyMS != nil {
					latencies = append(latencies, *item.LatencyMS)
				}
			}
		}
		reports = append(reports, report)
	}
	result := map[string]any{
		"report_kind":             "m1-model-evaluation-provider-telemetry",
		"run_id":                  input.RunID,
		"latency_distribution_ms": summarizeLatencies(latencies),
		"samples":                 reports,
	}
	encoded, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return fmt.Errorf("encode evaluation report: %w", err)
	}
	encoded = append(encoded, '\n')
	file, err := os.OpenFile(outputPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return fmt.Errorf("create evaluation report: %w", err)
	}
	if _, err := file.Write(encoded); err != nil {
		_ = file.Close()
		return fmt.Errorf("write evaluation report: %w", err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close evaluation report: %w", err)
	}
	return nil
}

func openReadOnlyDatabase(path string) (*sql.DB, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("resolve database path: %w", err)
	}
	location := &url.URL{Scheme: "file", Path: absolute}
	query := location.Query()
	query.Set("mode", "ro")
	query.Add("_pragma", "foreign_keys(1)")
	location.RawQuery = query.Encode()
	database, err := sql.Open("sqlite", location.String())
	if err != nil {
		return nil, fmt.Errorf("open evaluation database: %w", err)
	}
	if err := database.Ping(); err != nil {
		_ = database.Close()
		return nil, fmt.Errorf("ping evaluation database: %w", err)
	}
	return database, nil
}

func queryAiRuns(ctx context.Context, database *sql.DB, jobID string) ([]aiRunRecord, error) {
	rows, err := database.QueryContext(ctx, `
		SELECT id, outcome, coalesce(error_code, ''), provider_config_version,
		       provider_config_fingerprint, model, prompt_version, extraction_schema_version,
		       provider_schema_version, provider_schema_sha256,
		       claim_schema_version, claim_mapper_version,
		       input_processing_version, coalesce(request_hash, ''), coalesce(response_hash, ''),
		       input_tokens, output_tokens, latency_ms, started_at, coalesce(finished_at, '')
		FROM ai_runs WHERE job_id = ? ORDER BY started_at, id
	`, jobID)
	if err != nil {
		return nil, fmt.Errorf("query AI runs: %w", err)
	}
	defer rows.Close()
	result := make([]aiRunRecord, 0)
	for rows.Next() {
		var item aiRunRecord
		var inputTokens, outputTokens, latency sql.NullInt64
		if err := rows.Scan(
			&item.ID, &item.Outcome, &item.ErrorCode, &item.ProviderConfigVersion,
			&item.ProviderConfigFingerprint, &item.Model, &item.PromptVersion, &item.ExtractionSchemaVersion,
			&item.ProviderSchemaVersion, &item.ProviderSchemaSHA256,
			&item.ClaimSchemaVersion, &item.ClaimMapperVersion,
			&item.InputProcessingVersion, &item.RequestHash, &item.ResponseHash,
			&inputTokens, &outputTokens, &latency, &item.StartedAt, &item.FinishedAt,
		); err != nil {
			return nil, fmt.Errorf("scan AI run: %w", err)
		}
		item.InputTokens = nullableInt64(inputTokens)
		item.OutputTokens = nullableInt64(outputTokens)
		item.LatencyMS = nullableInt64(latency)
		result = append(result, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate AI runs: %w", err)
	}
	return result, nil
}

func nullableInt64(value sql.NullInt64) *int64 {
	if !value.Valid {
		return nil
	}
	result := value.Int64
	return &result
}

func summarizeLatencies(values []int64) latencySummary {
	if len(values) == 0 {
		return latencySummary{}
	}
	sort.Slice(values, func(left, right int) bool { return values[left] < values[right] })
	return latencySummary{
		Count: len(values),
		Min:   values[0],
		P50:   nearestRank(values, 0.50),
		P95:   nearestRank(values, 0.95),
		Max:   values[len(values)-1],
	}
}

func nearestRank(values []int64, percentile float64) int64 {
	index := int(math.Ceil(percentile*float64(len(values)))) - 1
	if index < 0 {
		index = 0
	}
	return values[index]
}
