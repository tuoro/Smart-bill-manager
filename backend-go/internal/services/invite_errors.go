package services

import "errors"

var (
	ErrNotFound   = errors.New("not_found")
	ErrInviteUsed = errors.New("invite_used")
)

