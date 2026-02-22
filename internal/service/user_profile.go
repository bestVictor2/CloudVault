package service

import (
	"CloudVault/internal/dto"
	"CloudVault/internal/repo"
	"CloudVault/model"
	"CloudVault/utils"
	"context"
	"errors"
	"strings"
	"time"

	"gorm.io/gorm"
)

// UserProfile represents editable profile data for the current user.
type UserProfile struct {
	ID         uint64    `json:"id"`
	UserName   string    `json:"user_name"`
	NickName   string    `json:"nick_name"`
	Email      string    `json:"email"`
	AvatarURL  string    `json:"avatar_url"`
	Bio        string    `json:"bio"`
	IsActive   bool      `json:"is_active"`
	TotalSpace uint64    `json:"total_space"`
	UseSpace   uint64    `json:"use_space"`
	CreatedAt  time.Time `json:"created_at"`
}

func toUserProfile(user *model.User) *UserProfile {
	if user == nil {
		return nil
	}
	return &UserProfile{
		ID:         user.ID,
		UserName:   user.UserName,
		NickName:   user.NickName,
		Email:      user.Email,
		AvatarURL:  user.AvatarURL,
		Bio:        user.Bio,
		IsActive:   user.IsActive,
		TotalSpace: user.TotalSpace,
		UseSpace:   user.UseSpace,
		CreatedAt:  user.CreatedAt,
	}
}

// GetUserProfileByID returns profile data by user ID.
func GetUserProfileByID(userID uint64) (*UserProfile, error) {
	if cached, ok := utils.GetUserInfoFromCache(context.Background(), userID); ok && cached != nil {
		return toUserProfile(cached), nil
	}

	var user model.User
	if err := repo.Db.Where("id = ?", userID).First(&user).Error; err != nil {
		return nil, err
	}
	_ = utils.SetUserInfoToCache(context.Background(), user.ID, &user, userInfoCacheTTL)
	return toUserProfile(&user), nil
}

// UpdateUserProfile updates editable fields for one user.
func UpdateUserProfile(userID uint64, req *dto.UpdateUserProfileRequest) (*UserProfile, error) {
	if req == nil {
		return nil, errors.New("invalid request")
	}

	updates := make(map[string]interface{})
	if req.NickName != nil {
		updates["nick_name"] = strings.TrimSpace(*req.NickName)
	}
	if req.AvatarURL != nil {
		updates["avatar_url"] = strings.TrimSpace(*req.AvatarURL)
	}
	if req.Bio != nil {
		updates["bio"] = strings.TrimSpace(*req.Bio)
	}
	if req.Email != nil {
		email := strings.TrimSpace(*req.Email)
		if email == "" {
			return nil, errors.New("email cannot be empty")
		}
		var count int64
		if err := repo.Db.Model(&model.User{}).
			Where("email = ? AND id <> ?", email, userID).
			Count(&count).Error; err != nil {
			return nil, err
		}
		if count > 0 {
			return nil, errors.New("email already exists")
		}
		updates["email"] = email
	}

	if len(updates) > 0 {
		result := repo.Db.Model(&model.User{}).Where("id = ?", userID).Updates(updates)
		if result.Error != nil {
			return nil, result.Error
		}
		if result.RowsAffected == 0 {
			return nil, gorm.ErrRecordNotFound
		}
	}

	_ = utils.InvalidateUserInfoCache(context.Background(), userID)
	return GetUserProfileByID(userID)
}
