package services

import (
	"fmt"
	"strings"

	"smart-bill-manager/internal/models"

	"gorm.io/gorm"
)

type AssignmentChangeSummary struct {
	RangeStartTs          int64 `json:"range_start_ts"`
	RangeEndTs            int64 `json:"range_end_ts"`
	AutoAssigned          int   `json:"auto_assigned"`
	AutoUnassigned        int   `json:"auto_unassigned"`
	ManualBlockedOverlaps int   `json:"manual_blocked_overlaps"`
}

type paymentMatchRow struct {
	PaymentID   string  `gorm:"column:payment_id"`
	CurrentTrip *string `gorm:"column:current_trip_id"`
	BadDebt     bool    `gorm:"column:bad_debt"`
	Count       int64   `gorm:"column:cnt"`
	OnlyTripID  *string `gorm:"column:only_trip_id"`
}

func autoAssignPaymentTx(tx *gorm.DB, payment *models.Payment) error {
	if tx == nil {
		return fmt.Errorf("tx is required")
	}
	if payment == nil {
		return fmt.Errorf("payment is required")
	}

	src := strings.TrimSpace(payment.TripAssignSrc)
	if src == "" {
		src = assignSrcAuto
	}
	if src == assignSrcBlocked {
		return tx.Model(&models.Payment{}).
			Where("id = ?", payment.ID).
			Updates(map[string]interface{}{
				"trip_id":                nil,
				"trip_assignment_source": assignSrcBlocked,
				"trip_assignment_state":  assignStateBlocked,
			}).Error
	}
	if src == assignSrcManual {
		// Manual is trusted; just ensure state reflects whether it's assigned.
		state := assignStateNoMatch
		if payment.TripID != nil && strings.TrimSpace(*payment.TripID) != "" {
			state = assignStateAssigned
		}
		return tx.Model(&models.Payment{}).
			Where("id = ?", payment.ID).
			Updates(map[string]interface{}{
				"trip_assignment_source": assignSrcManual,
				"trip_assignment_state":  state,
			}).Error
	}

	// Auto assignment.
	var trips []models.Trip
	if err := tx.Model(&models.Trip{}).
		Where("start_time_ts <= ? AND end_time_ts > ?", payment.TransactionTimeTs, payment.TransactionTimeTs).
		Order("start_time_ts DESC").
		Find(&trips).Error; err != nil {
		return err
	}

	updates := map[string]interface{}{
		"trip_assignment_source": assignSrcAuto,
		"trip_id":                nil,
		"trip_assignment_state":  assignStateNoMatch,
	}
	if len(trips) == 1 {
		updates["trip_id"] = trips[0].ID
		updates["trip_assignment_state"] = assignStateAssigned
	} else if len(trips) > 1 {
		updates["trip_assignment_state"] = assignStateOverlap
	}

	if err := tx.Model(&models.Payment{}).Where("id = ?", payment.ID).Updates(updates).Error; err != nil {
		return err
	}

	return nil
}

func recomputeAutoAssignmentsForRangeTx(tx *gorm.DB, startTs, endTs int64) (*AssignmentChangeSummary, []string, error) {
	if tx == nil {
		return nil, nil, fmt.Errorf("tx is required")
	}
	if endTs < startTs {
		return nil, nil, fmt.Errorf("invalid range")
	}

	out := &AssignmentChangeSummary{RangeStartTs: startTs, RangeEndTs: endTs}
	affectedBadDebtTrips := make(map[string]struct{})

	// For auto payments within range, compute how many trips match and the single trip_id when unique.
	var rows []paymentMatchRow
	if err := tx.
		Table("payments AS p").
		Select(`
			p.id AS payment_id,
			p.trip_id AS current_trip_id,
			p.bad_debt AS bad_debt,
			COUNT(t.id) AS cnt,
			MIN(t.id) AS only_trip_id
		`).
		Joins("LEFT JOIN trips AS t ON t.start_time_ts <= p.transaction_time_ts AND t.end_time_ts > p.transaction_time_ts").
		Where("p.trip_assignment_source = ? AND p.transaction_time_ts >= ? AND p.transaction_time_ts < ?", assignSrcAuto, startTs, endTs).
		Group("p.id, p.trip_id, p.bad_debt").
		Scan(&rows).Error; err != nil {
		return nil, nil, err
	}

	for _, r := range rows {
		curTrip := ""
		if r.CurrentTrip != nil {
			curTrip = strings.TrimSpace(*r.CurrentTrip)
		}

		nextTrip := ""
		nextState := assignStateNoMatch
		if r.Count == 1 && r.OnlyTripID != nil {
			nextTrip = strings.TrimSpace(*r.OnlyTripID)
			nextState = assignStateAssigned
		} else if r.Count > 1 {
			nextState = assignStateOverlap
		}

		// Determine whether we are unassigning or assigning.
		if curTrip != "" && nextTrip == "" {
			out.AutoUnassigned++
		}
		if curTrip == "" && nextTrip != "" {
			out.AutoAssigned++
		}
		if curTrip != "" && nextTrip != "" && curTrip != nextTrip {
			// trip changed due to edits; treat as assigned change
			out.AutoAssigned++
		}

		updates := map[string]interface{}{
			"trip_id":               nil,
			"trip_assignment_state": nextState,
		}
		if nextTrip != "" {
			updates["trip_id"] = nextTrip
		}

		if err := tx.Model(&models.Payment{}).Where("id = ?", r.PaymentID).Updates(updates).Error; err != nil {
			return nil, nil, err
		}

		if r.BadDebt {
			if curTrip != "" {
				affectedBadDebtTrips[curTrip] = struct{}{}
			}
			if nextTrip != "" {
				affectedBadDebtTrips[nextTrip] = struct{}{}
			}
		}
	}

	// Count manual/blocked payments that are currently inside overlaps (do not touch them).
	var manualOverlap int64
	if err := tx.
		Table("payments AS p").
		Joins("JOIN trips AS t ON t.start_time_ts <= p.transaction_time_ts AND t.end_time_ts > p.transaction_time_ts").
		Where("p.trip_assignment_source IN ? AND p.transaction_time_ts >= ? AND p.transaction_time_ts < ?", []string{assignSrcManual, assignSrcBlocked}, startTs, endTs).
		Group("p.id").
		Having("COUNT(t.id) > 1").
		Count(&manualOverlap).Error; err != nil {
		return nil, nil, err
	}
	out.ManualBlockedOverlaps = int(manualOverlap)

	tripIDs := make([]string, 0, len(affectedBadDebtTrips))
	for id := range affectedBadDebtTrips {
		tripIDs = append(tripIDs, id)
	}
	return out, tripIDs, nil
}
