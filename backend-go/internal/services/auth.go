package services

import (
	"errors"
	"log"

	"golang.org/x/crypto/bcrypt"
	"smart-bill-manager/internal/models"
	"smart-bill-manager/internal/repository"
	"smart-bill-manager/internal/utils"
)

type AuthService struct {
	userRepo *repository.UserRepository
}

func NewAuthService() *AuthService {
	return &AuthService{
		userRepo: repository.NewUserRepository(),
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
	// Check if username exists
	exists, err := s.userRepo.ExistsByUsername(username)
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

	if err := s.userRepo.Create(user); err != nil {
		return nil, err
	}

	// Generate token
	token, err := utils.GenerateToken(id, username, "user")
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
	user, err := s.userRepo.FindByUsername(username)
	if err != nil {
		return &AuthResult{Success: false, Message: "用户名或密码错误"}, nil
	}

	// Verify password
	if err := bcrypt.CompareHashAndPassword([]byte(user.Password), []byte(password)); err != nil {
		return &AuthResult{Success: false, Message: "用户名或密码错误"}, nil
	}

	// Generate token
	token, err := utils.GenerateToken(user.ID, user.Username, user.Role)
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
	return utils.VerifyToken(tokenString)
}

// GetUserByID gets a user by ID
func (s *AuthService) GetUserByID(id string) (*models.UserResponse, error) {
	user, err := s.userRepo.FindByID(id)
	if err != nil {
		return nil, err
	}
	userResponse := user.ToResponse()
	return &userResponse, nil
}

// GetAllUsers gets all users
func (s *AuthService) GetAllUsers() ([]models.UserResponse, error) {
	users, err := s.userRepo.FindAll()
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
	user, err := s.userRepo.FindByID(userID)
	if err != nil {
		return &AuthResult{Success: false, Message: "用户不存在"}, nil
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

	if err := s.userRepo.UpdatePassword(userID, string(hashedPassword)); err != nil {
		return nil, err
	}

	return &AuthResult{Success: true, Message: "密码修改成功"}, nil
}

// HasUsers checks if any users exist
func (s *AuthService) HasUsers() (bool, error) {
	count, err := s.userRepo.Count()
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

// CreateInitialAdmin creates the first admin user during setup
func (s *AuthService) CreateInitialAdmin(username, password string, email *string) (*AuthResult, error) {
	// Only allow if no users exist
	hasUsers, err := s.HasUsers()
	if err != nil {
		return nil, err
	}
	if hasUsers {
		return &AuthResult{Success: false, Message: "系统已初始化，无法重复设置"}, nil
	}

	// Validate username and password
	if len(username) < 3 || len(username) > 50 {
		return &AuthResult{Success: false, Message: "用户名长度应为3-50个字符"}, nil
	}

	if len(password) < 6 {
		return &AuthResult{Success: false, Message: "密码长度至少6个字符"}, nil
	}

	result, err := s.Register(username, password, email)
	if err != nil {
		return nil, err
	}

	if result.Success {
		// Update role to admin
		if err := s.userRepo.UpdateRole(username, "admin"); err != nil {
			return nil, err
		}

		log.Println("=========================================")
		log.Println("Admin user created via setup:")
		log.Printf("  Username: %s\n", username)
		log.Println("=========================================")

		// Update the role in the result
		if result.User != nil {
			result.User.Role = "admin"
		}
	}

	return result, nil
}

var ErrUnauthorized = errors.New("unauthorized")
