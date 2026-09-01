package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/tuoro/smart-bill-manager/apps/api/internal/adapters/emailmime"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/adapters/localstorage"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/adapters/postgresql"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/adapters/system"
	applicationemails "github.com/tuoro/smart-bill-manager/apps/api/internal/application/emails"
)

type emailFixtureResult struct {
	Kind             string `json:"kind"`
	Version          int    `json:"version"`
	ExerciseID       string `json:"exercise_id"`
	MessageID        string `json:"message_id"`
	AttachmentID     string `json:"attachment_id"`
	DocumentID       string `json:"document_id"`
	JobID            string `json:"job_id"`
	RawSHA256        string `json:"raw_sha256"`
	AttachmentSHA256 string `json:"attachment_sha256"`
	Passed           bool   `json:"passed"`
}

func archiveSyntheticEmail(ctx context.Context, options archiveOptions) (emailFixtureResult, error) {
	if !exerciseIDPattern.MatchString(options.ExerciseID) {
		return emailFixtureResult{}, errors.New("synthetic email exercise identity is invalid")
	}
	databaseConfig, err := postgresqladapter.RuntimeConfigFromEnvironment()
	if err != nil {
		return emailFixtureResult{}, err
	}
	image := syntheticAttachmentPNG()
	raw := buildSyntheticMIME(image)
	digest := sha256.Sum256(raw)
	imageDigest := sha256.Sum256(image)
	store, err := postgresqladapter.Open(ctx, databaseConfig)
	if err != nil {
		return emailFixtureResult{}, err
	}
	defer store.Close()
	objects, err := localstorage.New(options.Objects)
	if err != nil {
		return emailFixtureResult{}, err
	}
	inspector, err := localstorage.NewInspector(objects, options.PDFInfo)
	if err != nil {
		return emailFixtureResult{}, err
	}
	service := applicationemails.NewService(store, store, objects, inspector, emailmime.Parser{}, system.IDGenerator{}, system.Clock{})
	result, err := service.Archive(ctx, applicationemails.ArchiveInput{
		TenantID: options.TenantID, EmailSourceID: options.SourceID,
		ExternalMessageKey: hex.EncodeToString(digest[:]),
		ReceivedAt:         time.Now().UTC(), Raw: bytes.NewReader(raw),
		RequestID: "m4-recovery-email-fixture",
	})
	if err != nil {
		return emailFixtureResult{}, err
	}
	if result.Replayed || result.Status != "archived" || len(result.Attachments) != 1 || len(result.CreatedDocumentIDs) != 1 || len(result.CreatedJobIDs) != 1 {
		return emailFixtureResult{}, errors.New("synthetic email did not create exactly one archived attachment document")
	}
	attachment := result.Attachments[0]
	if attachment.DocumentID != result.CreatedDocumentIDs[0] || attachment.JobID != result.CreatedJobIDs[0] || attachment.ProcessingStatus != "queued" {
		return emailFixtureResult{}, errors.New("synthetic email attachment result is inconsistent")
	}
	return emailFixtureResult{
		Kind: "m4-recovery-email-fixture", Version: 1, ExerciseID: options.ExerciseID,
		MessageID: result.MessageID, AttachmentID: attachment.ID,
		DocumentID: attachment.DocumentID, JobID: attachment.JobID,
		RawSHA256: hex.EncodeToString(digest[:]), AttachmentSHA256: hex.EncodeToString(imageDigest[:]),
		Passed: true,
	}, nil
}

func syntheticAttachmentPNG() []byte {
	const encoded = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNk+A8AAQUBAScY42YAAAAASUVORK5CYII="
	content, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		panic("invalid embedded synthetic PNG: " + err.Error())
	}
	return content
}

func buildSyntheticMIME(image []byte) []byte {
	const boundary = "sbm-m4-recovery-boundary"
	encoded := base64.StdEncoding.EncodeToString(image)
	lines := make([]string, 0, (len(encoded)+75)/76)
	for len(encoded) > 76 {
		lines = append(lines, encoded[:76])
		encoded = encoded[76:]
	}
	if encoded != "" {
		lines = append(lines, encoded)
	}
	body := strings.Join(lines, "\r\n")
	return []byte(fmt.Sprintf("From: sender@example.invalid\r\nTo: archive@example.invalid\r\nSubject: M4 recovery fixture\r\nDate: Mon, 31 Aug 2026 00:00:00 +0000\r\nMIME-Version: 1.0\r\nContent-Type: multipart/mixed; boundary=%q\r\n\r\n--%s\r\nContent-Type: text/plain; charset=utf-8\r\n\r\nSynthetic recovery fixture.\r\n--%s\r\nContent-Type: image/png\r\nContent-Disposition: attachment; filename=\"recovery.png\"\r\nContent-Transfer-Encoding: base64\r\n\r\n%s\r\n--%s--\r\n", boundary, boundary, boundary, body, boundary))
}
