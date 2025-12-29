package services

import (
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

func (s *TripService) GetPendingPayments() ([]PendingPayment, error) {
	db := database.GetDB()

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
		ExtractedData      *string
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
			p.extracted_data AS extracted_data,
			t.id AS candidate_trip_id,
			t.name AS candidate_trip_name,
			t.start_time AS candidate_start_time,
			t.end_time AS candidate_end_time,
			t.timezone AS candidate_timezone
		`).
		Joins("JOIN trips AS t ON t.start_time_ts <= p.transaction_time_ts AND t.end_time_ts > p.transaction_time_ts").
		Where("p.trip_id IS NULL AND p.trip_assignment_source = ? AND p.trip_assignment_state = ?", assignSrcAuto, assignStateOverlap).
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
				ExtractedData:     r.ExtractedData,
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

func (s *TripService) AssignPendingPayment(paymentID, tripID string) error {
	paymentID = strings.TrimSpace(paymentID)
	tripID = strings.TrimSpace(tripID)
	if paymentID == "" || tripID == "" {
		return fmt.Errorf("payment_id and trip_id are required")
	}
	db := database.GetDB()
	if err := db.Model(&models.Payment{}).Where("id = ?", paymentID).Updates(map[string]interface{}{
		"trip_id":                tripID,
		"trip_assignment_source": assignSrcManual,
		"trip_assignment_state":  assignStateAssigned,
	}).Error; err != nil {
		return err
	}
	return recalcTripBadDebtLocked(tripID)
}

func (s *TripService) BlockPendingPayment(paymentID string) error {
	paymentID = strings.TrimSpace(paymentID)
	if paymentID == "" {
		return fmt.Errorf("payment_id is required")
	}
	db := database.GetDB()
	return db.Model(&models.Payment{}).Where("id = ?", paymentID).Updates(map[string]interface{}{
		"trip_id":                nil,
		"trip_assignment_source": assignSrcBlocked,
		"trip_assignment_state":  assignStateBlocked,
	}).Error
}
