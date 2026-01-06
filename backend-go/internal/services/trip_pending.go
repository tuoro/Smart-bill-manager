package services

import (
	"context"
	"fmt"
	"strings"

	"smart-bill-manager/internal/models"
	"smart-bill-manager/pkg/database"
)

type PendingCandidateTrip struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	StartTime string `json:"start_time"`
	EndTime   string `json:"end_time"`
	Timezone  string `json:"timezone"`
}

type PendingPayment struct {
	Payment    models.Payment         `json:"payment"`
	Candidates []PendingCandidateTrip `json:"candidates"`
}

func (s *TripService) GetPendingPayments(ownerUserID string) ([]PendingPayment, error) {
	return s.GetPendingPaymentsCtx(context.Background(), ownerUserID)
}

func (s *TripService) GetPendingPaymentsCtx(ctx context.Context, ownerUserID string) ([]PendingPayment, error) {
	ownerUserID = strings.TrimSpace(ownerUserID)
	if ownerUserID == "" {
		return nil, fmt.Errorf("missing owner_user_id")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	db := database.GetDB().WithContext(ctx)

	type row struct {
		PaymentID          string
		TripID             *string
		TripAssignSrc      string
		TripAssignState    string
		BadDebt            bool
		Amount             float64
		Merchant           *string
		Category           *string
		PaymentMethod      *string
		Description        *string
		TransactionTime    string
		TransactionTimeTs  int64
		ScreenshotPath     *string
		CandidateTripID    string
		CandidateTripName  string
		CandidateStartTime string
		CandidateEndTime   string
		CandidateTimezone  string
	}

	var rows []row
	if err := db.
		Table("payments AS p").
		Select(`
			p.id AS payment_id,
			p.trip_id AS trip_id,
			p.trip_assignment_source AS trip_assign_src,
			p.trip_assignment_state AS trip_assign_state,
			p.bad_debt AS bad_debt,
			p.amount AS amount,
			p.merchant AS merchant,
			p.category AS category,
			p.payment_method AS payment_method,
			p.description AS description,
			p.transaction_time AS transaction_time,
			p.transaction_time_ts AS transaction_time_ts,
			p.screenshot_path AS screenshot_path,
			t.id AS candidate_trip_id,
			t.name AS candidate_trip_name,
			t.start_time AS candidate_start_time,
			t.end_time AS candidate_end_time,
			t.timezone AS candidate_timezone
		`).
		Joins("JOIN trips AS t ON t.owner_user_id = p.owner_user_id AND t.start_time_ts <= p.transaction_time_ts AND t.end_time_ts > p.transaction_time_ts").
		Where("p.is_draft = 0").
		Where("p.owner_user_id = ?", ownerUserID).
		Where(
			`
			p.trip_id IS NULL AND (
				(p.trip_assignment_source = ? AND p.trip_assignment_state = ?)
				OR
				(p.trip_assignment_source = ? AND p.trip_assignment_state = ?)
			)
			`,
			assignSrcAuto,
			assignStateOverlap,
			assignSrcManual,
			assignStateNoMatch,
		).
		Order("p.transaction_time_ts DESC, p.id").
		Scan(&rows).Error; err != nil {
		return nil, err
	}

	byPay := make(map[string]*PendingPayment)
	order := make([]string, 0)

	for _, r := range rows {
		pp := byPay[r.PaymentID]
		if pp == nil {
			p := models.Payment{
				ID:                r.PaymentID,
				TripID:            r.TripID,
				TripAssignSrc:     r.TripAssignSrc,
				TripAssignState:   r.TripAssignState,
				BadDebt:           r.BadDebt,
				Amount:            r.Amount,
				Merchant:          r.Merchant,
				Category:          r.Category,
				PaymentMethod:     r.PaymentMethod,
				Description:       r.Description,
				TransactionTime:   r.TransactionTime,
				TransactionTimeTs: r.TransactionTimeTs,
				ScreenshotPath:    r.ScreenshotPath,
			}
			pp = &PendingPayment{Payment: p}
			byPay[r.PaymentID] = pp
			order = append(order, r.PaymentID)
		}
		pp.Candidates = append(pp.Candidates, PendingCandidateTrip{
			ID:        r.CandidateTripID,
			Name:      r.CandidateTripName,
			StartTime: r.CandidateStartTime,
			EndTime:   r.CandidateEndTime,
			Timezone:  r.CandidateTimezone,
		})
	}

	out := make([]PendingPayment, 0, len(order))
	for _, id := range order {
		out = append(out, *byPay[id])
	}
	return out, nil
}

func (s *TripService) AssignPendingPayment(ownerUserID string, paymentID, tripID string) error {
	ownerUserID = strings.TrimSpace(ownerUserID)
	paymentID = strings.TrimSpace(paymentID)
	tripID = strings.TrimSpace(tripID)
	if ownerUserID == "" || paymentID == "" || tripID == "" {
		return fmt.Errorf("owner_user_id, payment_id and trip_id are required")
	}
	db := database.GetDB()
	var t models.Trip
	if err := db.Select("id").Where("id = ? AND owner_user_id = ?", tripID, ownerUserID).First(&t).Error; err != nil {
		return fmt.Errorf("trip not found")
	}
	if err := db.Model(&models.Payment{}).Where("id = ? AND owner_user_id = ?", paymentID, ownerUserID).Updates(map[string]interface{}{
		"trip_id":                tripID,
		"trip_assignment_source": assignSrcManual,
		"trip_assignment_state":  assignStateAssigned,
	}).Error; err != nil {
		return err
	}
	return recalcTripBadDebtLocked(tripID)
}

func (s *TripService) BlockPendingPayment(ownerUserID string, paymentID string) error {
	ownerUserID = strings.TrimSpace(ownerUserID)
	paymentID = strings.TrimSpace(paymentID)
	if ownerUserID == "" || paymentID == "" {
		return fmt.Errorf("owner_user_id and payment_id are required")
	}
	db := database.GetDB()
	return db.Model(&models.Payment{}).Where("id = ? AND owner_user_id = ?", paymentID, ownerUserID).Updates(map[string]interface{}{
		"trip_id":                nil,
		"trip_assignment_source": assignSrcBlocked,
		"trip_assignment_state":  assignStateBlocked,
	}).Error
}
