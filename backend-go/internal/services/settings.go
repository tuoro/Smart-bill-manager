package services

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"smart-bill-manager/internal/models"
	"smart-bill-manager/pkg/database"

	"gorm.io/gorm"
)

const systemSettingsKey = "system_settings_v1"

type OCRSettings struct {
	Engine         string `json:"engine"`
	WorkerMode     string `json:"worker_mode"`
	MaxConcurrency int    `json:"max_concurrency"`
	TimeoutMs      int    `json:"timeout_ms"`
}

type DedupeSettings struct {
	StrictHashReject              bool `json:"strict_hash_reject"`
	SoftEnabled                   bool `json:"soft_enabled"`
	PaymentAmountTimeWindowMin    int  `json:"payment_amount_time_window_minutes"`
	InvoiceNumberMaxCandidates    int  `json:"invoice_number_max_candidates"`
	PaymentAmountTimeMaxCandidates int `json:"payment_amount_time_max_candidates"`
}

type CleanupSettings struct {
	Enabled                bool `json:"enabled"`
	DraftTTLHours          int  `json:"draft_ttl_hours"`
	IntervalMinutes        int  `json:"interval_minutes"`
	OrphanFileTTLHours     int  `json:"orphan_file_ttl_hours"`
	MaxDeletePerRun        int  `json:"max_delete_per_run"`
}

type SystemSettings struct {
	OCR    OCRSettings    `json:"ocr"`
	Dedupe DedupeSettings `json:"dedupe"`
	Cleanup CleanupSettings `json:"cleanup"`
}

type OCRSettingsPatch struct {
	Engine         *string `json:"engine"`
	WorkerMode     *string `json:"worker_mode"`
	MaxConcurrency *int    `json:"max_concurrency"`
	TimeoutMs      *int    `json:"timeout_ms"`
}

type DedupeSettingsPatch struct {
	StrictHashReject               *bool `json:"strict_hash_reject"`
	SoftEnabled                    *bool `json:"soft_enabled"`
	PaymentAmountTimeWindowMin     *int  `json:"payment_amount_time_window_minutes"`
	PaymentAmountTimeMaxCandidates *int  `json:"payment_amount_time_max_candidates"`
	InvoiceNumberMaxCandidates     *int  `json:"invoice_number_max_candidates"`
}

type CleanupSettingsPatch struct {
	Enabled            *bool `json:"enabled"`
	DraftTTLHours      *int  `json:"draft_ttl_hours"`
	IntervalMinutes    *int  `json:"interval_minutes"`
	OrphanFileTTLHours *int  `json:"orphan_file_ttl_hours"`
	MaxDeletePerRun    *int  `json:"max_delete_per_run"`
}

type SystemSettingsPatch struct {
	OCR     *OCRSettingsPatch     `json:"ocr"`
	Dedupe  *DedupeSettingsPatch  `json:"dedupe"`
	Cleanup *CleanupSettingsPatch `json:"cleanup"`
}

type settingsCache struct {
	mu       sync.RWMutex
	loadedAt time.Time
	val      SystemSettings
	ok       bool
}

var sysSettingsCache settingsCache

func invalidateSystemSettingsCache() {
	sysSettingsCache.mu.Lock()
	sysSettingsCache.ok = false
	sysSettingsCache.mu.Unlock()
}

func envInt(key string, fallback int) int {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return fallback
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return fallback
	}
	return n
}

func defaultSystemSettings() SystemSettings {
	ttl := envInt("SBM_DRAFT_TTL_HOURS", 6)
	interval := envInt("SBM_DRAFT_CLEANUP_INTERVAL_MINUTES", 15)
	enabled := ttl > 0 && interval > 0

	return SystemSettings{
		OCR: OCRSettings{
			Engine:         strings.ToLower(strings.TrimSpace(os.Getenv("SBM_OCR_ENGINE"))),
			WorkerMode:     "process",
			MaxConcurrency: 2,
			TimeoutMs:      60_000,
		},
		Dedupe: DedupeSettings{
			StrictHashReject:               true,
			SoftEnabled:                    true,
			PaymentAmountTimeWindowMin:     5,
			PaymentAmountTimeMaxCandidates: 5,
			InvoiceNumberMaxCandidates:     5,
		},
		Cleanup: CleanupSettings{
			Enabled:            enabled,
			DraftTTLHours:      ttl,
			IntervalMinutes:    interval,
			OrphanFileTTLHours: 0,
			MaxDeletePerRun:    200,
		},
	}
}

func normalizeOCRConfig(cfg SystemSettings) SystemSettings {
	cfg.OCR.Engine = strings.ToLower(strings.TrimSpace(cfg.OCR.Engine))
	if cfg.OCR.Engine == "" {
		cfg.OCR.Engine = "rapidocr"
	}
	if cfg.OCR.Engine != "rapidocr" {
		cfg.OCR.Engine = "rapidocr"
	}

	cfg.OCR.WorkerMode = strings.ToLower(strings.TrimSpace(cfg.OCR.WorkerMode))
	if cfg.OCR.WorkerMode == "" {
		cfg.OCR.WorkerMode = "process"
	}
	if cfg.OCR.WorkerMode != "process" && cfg.OCR.WorkerMode != "worker" {
		cfg.OCR.WorkerMode = "process"
	}
	if cfg.OCR.MaxConcurrency < 1 {
		cfg.OCR.MaxConcurrency = 1
	}
	if cfg.OCR.MaxConcurrency > 16 {
		cfg.OCR.MaxConcurrency = 16
	}
	if cfg.OCR.TimeoutMs < 1000 {
		cfg.OCR.TimeoutMs = 1000
	}
	if cfg.OCR.TimeoutMs > 300_000 {
		cfg.OCR.TimeoutMs = 300_000
	}

	if cfg.Dedupe.PaymentAmountTimeWindowMin < 0 {
		cfg.Dedupe.PaymentAmountTimeWindowMin = 0
	}
	if cfg.Dedupe.PaymentAmountTimeWindowMin > 60 {
		cfg.Dedupe.PaymentAmountTimeWindowMin = 60
	}
	if cfg.Dedupe.PaymentAmountTimeMaxCandidates < 1 {
		cfg.Dedupe.PaymentAmountTimeMaxCandidates = 1
	}
	if cfg.Dedupe.PaymentAmountTimeMaxCandidates > 20 {
		cfg.Dedupe.PaymentAmountTimeMaxCandidates = 20
	}
	if cfg.Dedupe.InvoiceNumberMaxCandidates < 1 {
		cfg.Dedupe.InvoiceNumberMaxCandidates = 1
	}
	if cfg.Dedupe.InvoiceNumberMaxCandidates > 20 {
		cfg.Dedupe.InvoiceNumberMaxCandidates = 20
	}

	if cfg.Cleanup.DraftTTLHours < 0 {
		cfg.Cleanup.DraftTTLHours = 0
	}
	if cfg.Cleanup.DraftTTLHours > 24*90 {
		cfg.Cleanup.DraftTTLHours = 24 * 90
	}
	if cfg.Cleanup.IntervalMinutes < 0 {
		cfg.Cleanup.IntervalMinutes = 0
	}
	if cfg.Cleanup.IntervalMinutes > 24*60 {
		cfg.Cleanup.IntervalMinutes = 24 * 60
	}
	if cfg.Cleanup.OrphanFileTTLHours < 0 {
		cfg.Cleanup.OrphanFileTTLHours = 0
	}
	if cfg.Cleanup.OrphanFileTTLHours > 24*90 {
		cfg.Cleanup.OrphanFileTTLHours = 24 * 90
	}
	if cfg.Cleanup.MaxDeletePerRun < 10 {
		cfg.Cleanup.MaxDeletePerRun = 10
	}
	if cfg.Cleanup.MaxDeletePerRun > 5000 {
		cfg.Cleanup.MaxDeletePerRun = 5000
	}

	return cfg
}

func GetSystemSettings() (SystemSettings, error) {
	sysSettingsCache.mu.RLock()
	if sysSettingsCache.ok && time.Since(sysSettingsCache.loadedAt) < 5*time.Second {
		v := sysSettingsCache.val
		sysSettingsCache.mu.RUnlock()
		return v, nil
	}
	sysSettingsCache.mu.RUnlock()

	db := database.GetDB()
	if db == nil {
		v := normalizeOCRConfig(defaultSystemSettings())
		sysSettingsCache.mu.Lock()
		sysSettingsCache.val = v
		sysSettingsCache.ok = true
		sysSettingsCache.loadedAt = time.Now()
		sysSettingsCache.mu.Unlock()
		return v, nil
	}

	var row models.SystemSetting
	err := db.Where("key = ?", systemSettingsKey).First(&row).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			v := normalizeOCRConfig(defaultSystemSettings())
			sysSettingsCache.mu.Lock()
			sysSettingsCache.val = v
			sysSettingsCache.ok = true
			sysSettingsCache.loadedAt = time.Now()
			sysSettingsCache.mu.Unlock()
			return v, nil
		}
		return SystemSettings{}, err
	}

	v := defaultSystemSettings()
	if strings.TrimSpace(row.ValueJSON) != "" {
		_ = json.Unmarshal([]byte(row.ValueJSON), &v)
	}
	v = normalizeOCRConfig(v)

	sysSettingsCache.mu.Lock()
	sysSettingsCache.val = v
	sysSettingsCache.ok = true
	sysSettingsCache.loadedAt = time.Now()
	sysSettingsCache.mu.Unlock()
	return v, nil
}

func UpdateSystemSettings(updatedBy string, patch SystemSettingsPatch) (SystemSettings, error) {
	db := database.GetDB()
	if db == nil {
		return SystemSettings{}, fmt.Errorf("database not initialized")
	}

	current, err := GetSystemSettings()
	if err != nil {
		current = defaultSystemSettings()
	}

	if patch.OCR != nil {
		if patch.OCR.Engine != nil {
			current.OCR.Engine = *patch.OCR.Engine
		}
		if patch.OCR.WorkerMode != nil {
			current.OCR.WorkerMode = *patch.OCR.WorkerMode
		}
		if patch.OCR.MaxConcurrency != nil {
			current.OCR.MaxConcurrency = *patch.OCR.MaxConcurrency
		}
		if patch.OCR.TimeoutMs != nil {
			current.OCR.TimeoutMs = *patch.OCR.TimeoutMs
		}
	}

	if patch.Dedupe != nil {
		if patch.Dedupe.StrictHashReject != nil {
			current.Dedupe.StrictHashReject = *patch.Dedupe.StrictHashReject
		}
		if patch.Dedupe.SoftEnabled != nil {
			current.Dedupe.SoftEnabled = *patch.Dedupe.SoftEnabled
		}
		if patch.Dedupe.PaymentAmountTimeWindowMin != nil {
			current.Dedupe.PaymentAmountTimeWindowMin = *patch.Dedupe.PaymentAmountTimeWindowMin
		}
		if patch.Dedupe.PaymentAmountTimeMaxCandidates != nil {
			current.Dedupe.PaymentAmountTimeMaxCandidates = *patch.Dedupe.PaymentAmountTimeMaxCandidates
		}
		if patch.Dedupe.InvoiceNumberMaxCandidates != nil {
			current.Dedupe.InvoiceNumberMaxCandidates = *patch.Dedupe.InvoiceNumberMaxCandidates
		}
	}

	if patch.Cleanup != nil {
		if patch.Cleanup.Enabled != nil {
			current.Cleanup.Enabled = *patch.Cleanup.Enabled
		}
		if patch.Cleanup.DraftTTLHours != nil {
			current.Cleanup.DraftTTLHours = *patch.Cleanup.DraftTTLHours
		}
		if patch.Cleanup.IntervalMinutes != nil {
			current.Cleanup.IntervalMinutes = *patch.Cleanup.IntervalMinutes
		}
		if patch.Cleanup.OrphanFileTTLHours != nil {
			current.Cleanup.OrphanFileTTLHours = *patch.Cleanup.OrphanFileTTLHours
		}
		if patch.Cleanup.MaxDeletePerRun != nil {
			current.Cleanup.MaxDeletePerRun = *patch.Cleanup.MaxDeletePerRun
		}
	}

	current = normalizeOCRConfig(current)

	b, err := json.Marshal(current)
	if err != nil {
		return SystemSettings{}, fmt.Errorf("marshal settings: %w", err)
	}

	uid := strings.TrimSpace(updatedBy)
	if uid == "" {
		uid = "admin"
	}

	err = db.Transaction(func(tx *gorm.DB) error {
		var row models.SystemSetting
		if e := tx.Where("key = ?", systemSettingsKey).First(&row).Error; e != nil {
			if errors.Is(e, gorm.ErrRecordNotFound) {
				row = models.SystemSetting{Key: systemSettingsKey}
			} else {
				return e
			}
		}
		row.ValueJSON = string(b)
		row.UpdatedBy = &uid
		return tx.Save(&row).Error
	})
	if err != nil {
		return SystemSettings{}, err
	}

	invalidateSystemSettingsCache()
	return current, nil
}
