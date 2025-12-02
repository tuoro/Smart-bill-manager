package services

import (
	"errors"
	"log"

	"golang.org/x/crypto/bcrypt"
	"smart-bill-manager/internal/config"
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
	Success bool                  `json:"success"`
	Message string                `json:"message"`
	User    *models.UserResponse  `json:"user,omitempty"`
	Token   string                `json:"token,omitempty"`
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

// EnsureAdminExists creates default admin if no users exist
func (s *AuthService) EnsureAdminExists() error {
	hasUsers, err := s.HasUsers()
	if err != nil {
		return err
	}

	if !hasUsers {
		// Get or generate admin password
		adminPassword := config.AppConfig.AdminPassword
		isRandomPassword := adminPassword == ""
		
		if isRandomPassword {
			adminPassword, err = utils.GenerateSecurePassword(12)
			if err != nil {
				return err
			}
		}

		log.Println("No users found, creating default admin user...")

		email := "admin@localhost"
		result, err := s.Register("admin", adminPassword, &email)
		if err != nil {
			return err
		}

		if result.Success {
			// Update role to admin
			if err := s.userRepo.UpdateRole("admin", "admin"); err != nil {
				return err
			}

			log.Println("=========================================")
			log.Println("Default admin user created:")
			log.Println("  Username: admin")
			if isRandomPassword {
				log.Printf("  Password: %s\n", adminPassword)
				log.Println("⚠️ IMPORTANT: Save this password! It will not be shown again.")
			} else {
				log.Println("  Password: (from ADMIN_PASSWORD environment variable)")
			}
			log.Println("=========================================")
		}
	}

	return nil
}

var ErrUnauthorized = errors.New("unauthorized")
