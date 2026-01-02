package services

import (
	"encoding/json"
	"errors"
	"log"
	"os"
	"strconv"
	"strings"
	"time"

	"smart-bill-manager/internal/models"
	"smart-bill-manager/internal/utils"

	"gorm.io/gorm"
)

const (
	TaskTypePaymentOCR = "payment_ocr"
	TaskTypeInvoiceOCR = "invoice_ocr"

	TaskStatusQueued     = "queued"
	TaskStatusProcessing = "processing"
	TaskStatusSucceeded  = "succeeded"
	TaskStatusFailed     = "failed"
	TaskStatusCanceled   = "canceled"
)

type TaskService struct {
	db            *gorm.DB
	paymentSvc    *PaymentService
	invoiceSvc    *InvoiceService
	pollInterval  time.Duration
}

func NewTaskService(db *gorm.DB, paymentSvc *PaymentService, invoiceSvc *InvoiceService) *TaskService {
	return &TaskService{
		db:           db,
		paymentSvc:   paymentSvc,
		invoiceSvc:   invoiceSvc,
		pollInterval: 800 * time.Millisecond,
	}
}

func (s *TaskService) CreateTask(taskType string, createdBy string, targetID string, fileSHA256 *string) (*models.Task, error) {
	if s.db == nil {
		return nil, errors.New("db not initialized")
	}
	taskType = strings.TrimSpace(taskType)
	createdBy = strings.TrimSpace(createdBy)
	targetID = strings.TrimSpace(targetID)
	if taskType == "" || createdBy == "" || targetID == "" {
		return nil, errors.New("missing fields")
	}
	if fileSHA256 != nil {
		sha := strings.TrimSpace(*fileSHA256)
		if sha == "" {
			fileSHA256 = nil
		} else {
			fileSHA256 = &sha
		}
	}

	var existing models.Task
	q := s.db.
		Where("type = ? AND created_by = ? AND target_id = ? AND status IN ?",
			taskType,
			createdBy,
			targetID,
			[]string{TaskStatusQueued, TaskStatusProcessing},
		)
	if fileSHA256 != nil {
		q = q.Where("file_sha256 = ?", *fileSHA256)
	}
	err := q.First(&existing).Error
	if err == nil {
		return &existing, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}

	t := &models.Task{
		ID:         utils.GenerateUUID(),
		Type:       taskType,
		Status:     TaskStatusQueued,
		CreatedBy:  createdBy,
		TargetID:   targetID,
		FileSHA256: fileSHA256,
	}
	if err := s.db.Create(t).Error; err != nil {
		return nil, err
	}
	return t, nil
}

func (s *TaskService) GetTask(id string) (*models.Task, error) {
	if s.db == nil {
		return nil, errors.New("db not initialized")
	}
	id = strings.TrimSpace(id)
	if id == "" {
		return nil, gorm.ErrRecordNotFound
	}
	var t models.Task
	if err := s.db.Where("id = ?", id).First(&t).Error; err != nil {
		return nil, err
	}
	return &t, nil
}

func (s *TaskService) CancelTask(id string, requester string) error {
	if s.db == nil {
		return errors.New("db not initialized")
	}
	id = strings.TrimSpace(id)
	requester = strings.TrimSpace(requester)
	if id == "" || requester == "" {
		return errors.New("invalid input")
	}

	res := s.db.Model(&models.Task{}).
		Where(
			"id = ? AND created_by = ? AND status IN ?",
			id,
			requester,
			[]string{TaskStatusQueued, TaskStatusProcessing},
		).
		Updates(map[string]any{
			"status":      TaskStatusCanceled,
			"result_json": nil,
			"error":       nil,
		})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		var t models.Task
		if err := s.db.Select("status", "created_by").Where("id = ?", id).First(&t).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return errors.New("task not found")
			}
			return err
		}
		if strings.TrimSpace(t.CreatedBy) == requester && t.Status == TaskStatusCanceled {
			return nil
		}
		return errors.New("task not cancelable")
	}
	return nil
}

func (s *TaskService) StartWorker() {
	if s.db == nil {
		return
	}
	processingTTL := getEnvSeconds("SBM_TASK_PROCESSING_TTL_SECONDS", 3600)
	reapInterval := getEnvSeconds("SBM_TASK_REAPER_INTERVAL_SECONDS", 30)
	if reapInterval < 5*time.Second {
		reapInterval = 5 * time.Second
	}
	if processingTTL < 30*time.Second {
		processingTTL = 30 * time.Second
	}

	log.Printf("[TaskWorker] started poll=%s ttl=%s reaper=%s", s.pollInterval, processingTTL, reapInterval)
	go func() {
		for {
			time.Sleep(s.pollInterval)
			if err := s.processOne(); err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
				log.Printf("[TaskWorker] process error: %v", err)
			}
		}
	}()
	go func() {
		for {
			time.Sleep(reapInterval)
			if err := s.reapStuckProcessing(processingTTL); err != nil {
				log.Printf("[TaskWorker] reaper error: %v", err)
			}
		}
	}()
}

func (s *TaskService) reapStuckProcessing(ttl time.Duration) error {
	cutoff := time.Now().Add(-ttl)
	msg := "task processing timeout"
	res := s.db.Model(&models.Task{}).
		Where("status = ? AND updated_at < ?", TaskStatusProcessing, cutoff).
		Updates(map[string]any{
			"status":      TaskStatusFailed,
			"result_json": nil,
			"error":       &msg,
		})
	return res.Error
}

func getEnvSeconds(key string, defaultSeconds int) time.Duration {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return time.Duration(defaultSeconds) * time.Second
	}
	n, err := strconv.Atoi(v)
	if err != nil || n <= 0 {
		return time.Duration(defaultSeconds) * time.Second
	}
	return time.Duration(n) * time.Second
}

func (s *TaskService) processOne() error {
	var t models.Task
	res := s.db.
		Where("status = ?", TaskStatusQueued).
		Order("created_at ASC, id ASC").
		Limit(1).
		Find(&t)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}

	// Claim the task.
	res = s.db.Model(&models.Task{}).
		Where("id = ? AND status = ?", t.ID, TaskStatusQueued).
		Updates(map[string]any{
			"status": TaskStatusProcessing,
		})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}

	// If canceled right after claiming, skip processing.
	var latest models.Task
	if err := s.db.Select("status").Where("id = ?", t.ID).First(&latest).Error; err == nil {
		if latest.Status == TaskStatusCanceled {
			return nil
		}
	}

	var (
		result any
		runErr error
	)
	switch t.Type {
	case TaskTypePaymentOCR:
		result, runErr = s.paymentSvc.ProcessPaymentOCRTask(t.TargetID)
	case TaskTypeInvoiceOCR:
		result, runErr = s.invoiceSvc.ProcessInvoiceOCRTask(t.TargetID)
	default:
		runErr = errors.New("unknown task type")
	}

	if runErr != nil {
		msg := runErr.Error()
		_ = s.db.Model(&models.Task{}).Where("id = ? AND status = ?", t.ID, TaskStatusProcessing).Updates(map[string]any{
			"status": TaskStatusFailed,
			"error":  &msg,
		}).Error
		return nil
	}

	var resultJSON *string
	if result != nil {
		if b, err := json.Marshal(result); err == nil {
			s := string(b)
			resultJSON = &s
		}
	}

	_ = s.db.Model(&models.Task{}).Where("id = ? AND status = ?", t.ID, TaskStatusProcessing).Updates(map[string]any{
		"status":      TaskStatusSucceeded,
		"result_json": resultJSON,
		"error":       nil,
	}).Error
	return nil
}
