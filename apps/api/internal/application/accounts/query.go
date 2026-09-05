package accounts

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"

	"github.com/tuoro/smart-bill-manager/apps/api/internal/domain"
	"github.com/tuoro/smart-bill-manager/apps/api/internal/ports"
)

type Page[T any] struct {
	Items      []T    `json:"items"`
	NextCursor string `json:"next_cursor"`
}

func (s Service) Member(ctx context.Context, principal ports.SessionPrincipal, id string) (ports.Member, error) {
	actor, err := accountActor(principal, s.clock.Now(), true)
	if err != nil {
		return ports.Member{}, err
	}
	return s.repository.GetMember(ctx, actor, id)
}

func (s Service) Invitation(ctx context.Context, principal ports.SessionPrincipal, id string) (ports.Invitation, error) {
	actor, err := accountActor(principal, s.clock.Now(), true)
	if err != nil {
		return ports.Invitation{}, err
	}
	return s.repository.GetInvitation(ctx, actor, id, s.clock.Now())
}

type accountCursor struct {
	Kind, TenantID string
	Key            domain.FactSortKey
}

func accountQuery(tenantID, kind, cursor string, limit int) (ports.AccountPageQuery, error) {
	query := ports.AccountPageQuery{Limit: limit}
	if limit < 1 || limit > 100 || len(cursor) > 2048 {
		return query, domain.ErrInvalidInput
	}
	if cursor == "" {
		return query, nil
	}
	payload, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil {
		return query, domain.ErrInvalidInput
	}
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	var value accountCursor
	if err := decoder.Decode(&value); err != nil {
		return query, domain.ErrInvalidInput
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) || value.Kind != kind || value.TenantID != tenantID || !value.Key.Valid() {
		return query, domain.ErrInvalidInput
	}
	query.After = &value.Key
	return query, nil
}

func nextAccountCursor(tenantID, kind string, key *domain.FactSortKey) (string, error) {
	if key == nil {
		return "", nil
	}
	payload, err := json.Marshal(accountCursor{Kind: kind, TenantID: tenantID, Key: *key})
	return base64.RawURLEncoding.EncodeToString(payload), err
}

func (s Service) Members(ctx context.Context, principal ports.SessionPrincipal, cursor string, limit int) (Page[ports.Member], error) {
	actor, err := accountActor(principal, s.clock.Now(), true)
	if err != nil {
		return Page[ports.Member]{}, err
	}
	query, err := accountQuery(actor.TenantID, "members/1", cursor, limit)
	if err != nil {
		return Page[ports.Member]{}, err
	}
	page, err := s.repository.ListMembers(ctx, actor, query, s.clock.Now())
	if err != nil {
		return Page[ports.Member]{}, err
	}
	next, err := nextAccountCursor(actor.TenantID, "members/1", page.Next)
	return Page[ports.Member]{Items: page.Items, NextCursor: next}, err
}

func (s Service) Invitations(ctx context.Context, principal ports.SessionPrincipal, cursor string, limit int) (Page[ports.Invitation], error) {
	actor, err := accountActor(principal, s.clock.Now(), true)
	if err != nil {
		return Page[ports.Invitation]{}, err
	}
	query, err := accountQuery(actor.TenantID, "invitations/1", cursor, limit)
	if err != nil {
		return Page[ports.Invitation]{}, err
	}
	page, err := s.repository.ListInvitations(ctx, actor, query, s.clock.Now())
	if err != nil {
		return Page[ports.Invitation]{}, err
	}
	next, err := nextAccountCursor(actor.TenantID, "invitations/1", page.Next)
	return Page[ports.Invitation]{Items: page.Items, NextCursor: next}, err
}
