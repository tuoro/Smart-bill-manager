package services

import (
	"context"
	"errors"
	"log"
	"sync"

	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"

	"smart-bill-manager/internal/models"
	"smart-bill-manager/internal/repository"
	"smart-bill-manager/internal/utils"
)

type AuthService struct {
	db           *gorm.DB
	userRepo     *repository.UserRepository
	tokenManager *utils.TokenManager
	setupMu      sync.Mutex
}

func NewAuthService(db *gorm.DB, tokenManager *utils.TokenManager) *AuthService {
	return &AuthService{
		db:           db,
		userRepo:     repository.NewUserRepository(db),
		tokenManager: tokenManager,
	}
}

type AuthResult struct {
	Success bool                 `json:"success"`
	Message string               `json:"message"`
	User    *models.UserResponse `json:"user,omitempty"`
	Token   string               `json:"token,omitempty"`
}

// Register creates a new user
func (s *AuthService) Register(username, password string, email *string) (*AuthResult, error) {
	return s.RegisterCtx(context.Background(), username, password, email)
}

func (s *AuthService) RegisterCtx(ctx context.Context, username, password string, email *string) (*AuthResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	// Check if username exists
	exists, err := s.userRepo.ExistsByUsernameCtx(ctx, username)
	if err != nil {
		return nil, err
	}
	if exists {
		return &AuthResult{Success: false, Message: "用户名已存在"}, nil
	}

	// Hash password
	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return nil, err
	}

	// Create user
	id := utils.GenerateUUID()
	user := &models.User{
		ID:       id,
		Username: username,
		Password: string(hashedPassword),
		Email:    email,
		Role:     "user",
		IsActive: 1,
	}

	if err := s.userRepo.CreateCtx(ctx, user); err != nil {
		return nil, err
	}

	// Generate token
	token, err := s.tokenManager.GenerateToken(id, username, "user")
	if err != nil {
		return nil, err
	}

	userResponse := user.ToResponse()
	return &AuthResult{
		Success: true,
		Message: "注册成功",
		User:    &userResponse,
		Token:   token,
	}, nil
}

// Login authenticates a user
func (s *AuthService) Login(username, password string) (*AuthResult, error) {
	return s.LoginCtx(context.Background(), username, password)
}

func (s *AuthService) LoginCtx(ctx context.Context, username, password string) (*AuthResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	user, err := s.userRepo.FindByUsernameCtx(ctx, username)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return &AuthResult{Success: false, Message: "用户名或密码错误"}, nil
		}
		return nil, err
	}

	if user.IsActive != 1 {
		return &AuthResult{Success: false, Message: "账号已停用"}, nil
	}

	// Verify password
	if err := bcrypt.CompareHashAndPassword([]byte(user.Password), []byte(password)); err != nil {
		return &AuthResult{Success: false, Message: "用户名或密码错误"}, nil
	}

	// Generate token
	token, err := s.tokenManager.GenerateToken(user.ID, user.Username, user.Role)
	if err != nil {
		return nil, err
	}

	userResponse := user.ToResponse()
	return &AuthResult{
		Success: true,
		Message: "登录成功",
		User:    &userResponse,
		Token:   token,
	}, nil
}

// VerifyToken verifies a JWT token
func (s *AuthService) VerifyToken(tokenString string) (*utils.Claims, error) {
	return s.tokenManager.VerifyToken(tokenString)
}

// GetUserByID gets a user by ID
func (s *AuthService) GetUserByID(id string) (*models.UserResponse, error) {
	return s.GetUserByIDCtx(context.Background(), id)
}

func (s *AuthService) GetUserByIDCtx(ctx context.Context, id string) (*models.UserResponse, error) {
	user, err := s.userRepo.FindByIDCtx(ctx, id)
	if err != nil {
		return nil, err
	}
	userResponse := user.ToResponse()
	return &userResponse, nil
}

// GetAllUsers gets all users
func (s *AuthService) GetAllUsers() ([]models.UserResponse, error) {
	return s.GetAllUsersCtx(context.Background())
}

func (s *AuthService) GetAllUsersCtx(ctx context.Context) ([]models.UserResponse, error) {
	users, err := s.userRepo.FindAllCtx(ctx)
	if err != nil {
		return nil, err
	}

	var responses []models.UserResponse
	for _, u := range users {
		responses = append(responses, u.ToResponse())
	}
	return responses, nil
}

// UpdatePassword updates user password
func (s *AuthService) UpdatePassword(userID, oldPassword, newPassword string) (*AuthResult, error) {
	return s.UpdatePasswordCtx(context.Background(), userID, oldPassword, newPassword)
}

func (s *AuthService) UpdatePasswordCtx(ctx context.Context, userID, oldPassword, newPassword string) (*AuthResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	user, err := s.userRepo.FindByIDCtx(ctx, userID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return &AuthResult{Success: false, Message: "用户不存在"}, nil
		}
		return nil, err
	}

	// Verify old password
	if err := bcrypt.CompareHashAndPassword([]byte(user.Password), []byte(oldPassword)); err != nil {
		return &AuthResult{Success: false, Message: "原密码错误"}, nil
	}

	// Hash new password
	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(newPassword), bcrypt.DefaultCost)
	if err != nil {
		return nil, err
	}

	if err := s.userRepo.UpdatePasswordCtx(ctx, userID, string(hashedPassword)); err != nil {
		return nil, err
	}

	return &AuthResult{Success: true, Message: "密码修改成功"}, nil
}

// HasUsers checks if any users exist
func (s *AuthService) HasUsers() (bool, error) {
	return s.HasUsersCtx(context.Background())
}

func (s *AuthService) HasUsersCtx(ctx context.Context) (bool, error) {
	count, err := s.userRepo.CountCtx(ctx)
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

// CreateInitialAdmin creates the first admin user during setup
func (s *AuthService) CreateInitialAdmin(username, password string, email *string) (*AuthResult, error) {
	return s.CreateInitialAdminCtx(context.Background(), username, password, email)
}

func (s *AuthService) CreateInitialAdminCtx(ctx context.Context, username, password string, email *string) (*AuthResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	s.setupMu.Lock()
	defer s.setupMu.Unlock()

	if len(username) < 3 || len(username) > 50 {
		return &AuthResult{Success: false, Message: "用户名长度应为3-50个字符"}, nil
	}
	if len(password) < 6 {
		return &AuthResult{Success: false, Message: "密码长度至少6个字符"}, nil
	}

	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return nil, err
	}

	var (
		createdUser        models.User
		alreadyInitialized bool
	)
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var count int64
		if err := tx.Model(&models.User{}).Count(&count).Error; err != nil {
			return err
		}
		if count > 0 {
			alreadyInitialized = true
			return nil
		}
		createdUser = models.User{
			ID:       utils.GenerateUUID(),
			Username: username,
			Password: string(hashedPassword),
			Email:    email,
			Role:     "admin",
			IsActive: 1,
		}
		return tx.Create(&createdUser).Error
	})
	if err != nil {
		return nil, err
	}
	if alreadyInitialized {
		return &AuthResult{Success: false, Message: "系统已初始化，无法重复设置"}, nil
	}

	token, err := s.tokenManager.GenerateToken(createdUser.ID, createdUser.Username, createdUser.Role)
	if err != nil {
		return nil, err
	}
	userResponse := createdUser.ToResponse()

	log.Printf("初始管理员已创建: username=%s", username)

	return &AuthResult{
		Success: true,
		Message: "初始化成功",
		User:    &userResponse,
		Token:   token,
	}, nil
}

var ErrUnauthorized = errors.New("unauthorized")
