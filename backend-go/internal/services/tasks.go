package services

import (
	"encoding/json"
	"errors"
	"log"
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
		Where("id = ? AND created_by = ? AND status = ?", id, requester, TaskStatusQueued).
		Updates(map[string]any{
			"status": TaskStatusCanceled,
			"error":  nil,
		})
	return res.Error
}

func (s *TaskService) StartWorker() {
	if s.db == nil {
		return
	}
	go func() {
		for {
			time.Sleep(s.pollInterval)
			if err := s.processOne(); err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
				log.Printf("[TaskWorker] process error: %v", err)
			}
		}
	}()
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
		_ = s.db.Model(&models.Task{}).Where("id = ?", t.ID).Updates(map[string]any{
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

	_ = s.db.Model(&models.Task{}).Where("id = ?", t.ID).Updates(map[string]any{
		"status":      TaskStatusSucceeded,
		"result_json": resultJSON,
		"error":       nil,
	}).Error
	return nil
}
