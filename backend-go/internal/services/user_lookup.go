package services

import (
	"context"
	"strings"
)

func (s *AuthService) GetUsernamesByIDsCtx(ctx context.Context, ids []string) (map[string]string, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	users, err := s.userRepo.FindUsernamesByIDsCtx(ctx, ids)
	if err != nil {
		return nil, err
	}
	m := make(map[string]string, len(users))
	for _, u := range users {
		id := strings.TrimSpace(u.ID)
		if id == "" {
			continue
		}
		m[id] = u.Username
	}
	return m, nil
}
