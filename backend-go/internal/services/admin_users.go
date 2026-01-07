package services

import (
	"context"
	"errors"
	"fmt"

	"gorm.io/gorm"

	"smart-bill-manager/internal/models"
	"smart-bill-manager/pkg/database"
)

var (
	ErrUserSelfAction      = errors.New("user_self_action")
	ErrUserLastAdmin       = errors.New("user_last_admin")
	ErrUserDeleteForbidden = errors.New("user_delete_forbidden")
)

type DeleteUserResult struct {
	UserID               string `json:"userId"`
	PaymentsDeleted      int64  `json:"paymentsDeleted"`
	InvoicesDeleted      int64  `json:"invoicesDeleted"`
	TripsDeleted         int64  `json:"tripsDeleted"`
	EmailConfigsDeleted  int64  `json:"emailConfigsDeleted"`
	EmailLogsDeleted     int64  `json:"emailLogsDeleted"`
	TasksDeleted         int64  `json:"tasksDeleted"`
	RegressionDeleted    int64  `json:"regressionSamplesDeleted"`
	PaymentOCRDeleted    int64  `json:"paymentOCRDeleted"`
	InvoiceOCRDeleted    int64  `json:"invoiceOCRDeleted"`
	LinksDeleted         int64  `json:"linksDeleted"`
	InvitesCreatedByUser int64  `json:"invitesCreatedByUser"`
	InvitesUsedByUser    int64  `json:"invitesUsedByUser"`
}

func (s *AuthService) SetUserActiveCtx(ctx context.Context, actorUserID, targetUserID string, active bool) (*models.UserResponse, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if actorUserID != "" && actorUserID == targetUserID {
		return nil, ErrUserSelfAction
	}

	u, err := s.userRepo.FindByIDCtx(ctx, targetUserID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}

	// Prevent locking out the system by disabling the last active admin.
	if u.Role == "admin" && !active {
		cnt, err := s.userRepo.CountActiveAdminsCtx(ctx)
		if err != nil {
			return nil, err
		}
		if cnt <= 1 && u.IsActive == 1 {
			return nil, ErrUserLastAdmin
		}
	}

	if err := s.userRepo.UpdateActiveByIDCtx(ctx, targetUserID, active); err != nil {
		return nil, err
	}

	updated, err := s.userRepo.FindByIDCtx(ctx, targetUserID)
	if err != nil {
		return nil, err
	}
	resp := updated.ToResponse()
	return &resp, nil
}

func (s *AuthService) DeleteUserCtx(ctx context.Context, actorUserID, targetUserID string) (*DeleteUserResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if actorUserID != "" && actorUserID == targetUserID {
		return nil, ErrUserSelfAction
	}

	u, err := s.userRepo.FindByIDCtx(ctx, targetUserID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}

	// Never allow deleting the last active admin.
	if u.Role == "admin" {
		cnt, err := s.userRepo.CountActiveAdminsCtx(ctx)
		if err != nil {
			return nil, err
		}
		if cnt <= 1 && u.IsActive == 1 {
			return nil, ErrUserLastAdmin
		}
	}

	out := &DeleteUserResult{UserID: targetUserID}
	db := database.GetDB().WithContext(ctx)
	if err := db.Transaction(func(tx *gorm.DB) error {
		// Links must be deleted first.
		res := tx.Exec(
			`DELETE FROM invoice_payment_links
			  WHERE payment_id IN (SELECT id FROM payments WHERE owner_user_id = ?)
			     OR invoice_id IN (SELECT id FROM invoices WHERE owner_user_id = ?)`,
			targetUserID,
			targetUserID,
		)
		if res.Error != nil {
			return res.Error
		}
		out.LinksDeleted = res.RowsAffected

		res = tx.Where("owner_user_id = ?", targetUserID).Delete(&models.PaymentOCRBlob{})
		if res.Error != nil {
			return res.Error
		}
		out.PaymentOCRDeleted = res.RowsAffected

		res = tx.Where("owner_user_id = ?", targetUserID).Delete(&models.InvoiceOCRBlob{})
		if res.Error != nil {
			return res.Error
		}
		out.InvoiceOCRDeleted = res.RowsAffected

		res = tx.Where("owner_user_id = ?", targetUserID).Delete(&models.Invoice{})
		if res.Error != nil {
			return res.Error
		}
		out.InvoicesDeleted = res.RowsAffected

		res = tx.Where("owner_user_id = ?", targetUserID).Delete(&models.Payment{})
		if res.Error != nil {
			return res.Error
		}
		out.PaymentsDeleted = res.RowsAffected

		res = tx.Where("owner_user_id = ?", targetUserID).Delete(&models.Trip{})
		if res.Error != nil {
			return res.Error
		}
		out.TripsDeleted = res.RowsAffected

		res = tx.Where("owner_user_id = ?", targetUserID).Delete(&models.EmailLog{})
		if res.Error != nil {
			return res.Error
		}
		out.EmailLogsDeleted = res.RowsAffected

		res = tx.Where("owner_user_id = ?", targetUserID).Delete(&models.EmailConfig{})
		if res.Error != nil {
			return res.Error
		}
		out.EmailConfigsDeleted = res.RowsAffected

		res = tx.Where("owner_user_id = ?", targetUserID).Delete(&models.Task{})
		if res.Error != nil {
			return res.Error
		}
		out.TasksDeleted = res.RowsAffected

		res = tx.Where("created_by = ? AND origin = ?", targetUserID, "ui").Delete(&models.RegressionSample{})
		if res.Error != nil {
			return res.Error
		}
		out.RegressionDeleted = res.RowsAffected

		// Keep invites, but return counts for UI visibility.
		res = tx.Model(&models.Invite{}).Where("created_by = ?", targetUserID).Count(&out.InvitesCreatedByUser)
		if res.Error != nil {
			return res.Error
		}
		res = tx.Model(&models.Invite{}).Where("used_by = ?", targetUserID).Count(&out.InvitesUsedByUser)
		if res.Error != nil {
			return res.Error
		}

		if err := tx.Delete(&models.User{}, "id = ?", targetUserID).Error; err != nil {
			return fmt.Errorf("delete user: %w", err)
		}
		return nil
	}); err != nil {
		return nil, err
	}

	return out, nil
}
