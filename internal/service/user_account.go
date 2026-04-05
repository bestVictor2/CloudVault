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
	"gorm.io/gorm/clause"
)

const userInfoCacheTTL = 5 * time.Minute

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

// FavoriteItem is one favorite file/folder with lightweight metadata.
type FavoriteItem struct {
	FileID    uint64     `json:"file_id"`
	Name      string     `json:"name"`
	IsDir     bool       `json:"is_dir"`
	Size      int64      `json:"size"`
	ParentID  *uint64    `json:"parent_id"`
	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt *time.Time `json:"updated_at,omitempty"`
}

// RecentItem is one recent access record.
type RecentItem struct {
	FileID       uint64    `json:"file_id"`
	Name         string    `json:"name"`
	IsDir        bool      `json:"is_dir"`
	Size         int64     `json:"size"`
	ParentID     *uint64   `json:"parent_id"`
	Source       string    `json:"source"`
	AccessCount  int64     `json:"access_count"`
	LastAccessAt time.Time `json:"last_access_at"`
}

// CommonDirItem is one directory ranked by recent usage.
type CommonDirItem struct {
	FileID       uint64    `json:"file_id"`
	Name         string    `json:"name"`
	ParentID     *uint64   `json:"parent_id"`
	AccessCount  int64     `json:"access_count"`
	LastAccessAt time.Time `json:"last_access_at"`
}

// CreateUser hashes password and creates a user.
func CreateUser(user *model.User) error {
	hashed, err := utils.GetPwd(user.Password)
	if err != nil {
		return err
	}
	user.Password = hashed
	if err := repo.Db.Create(user).Error; err != nil {
		return err
	}
	_ = utils.SetUserInfoToCache(context.Background(), user.ID, user, userInfoCacheTTL)
	return nil
}

// FindIdByUsername returns user ID by username.
func FindIdByUsername(username string) (uint64, error) {
	var user model.User
	if err := repo.Db.Model(&model.User{}).Where("user_name = ?", username).First(&user).Error; err != nil {
		return 0, err
	}
	return user.ID, nil
}

// FindUserNameById returns username by ID.
func FindUserNameById(userId uint64) (string, error) {
	if cached, ok := utils.GetUserInfoFromCache(context.Background(), userId); ok && cached != nil {
		return cached.UserName, nil
	}

	var user model.User
	if err := repo.Db.Model(&model.User{}).Where("id = ?", userId).First(&user).Error; err != nil {
		return "", err
	}
	_ = utils.SetUserInfoToCache(context.Background(), userId, &user, userInfoCacheTTL)
	return user.UserName, nil
}

// IsExist checks whether a user exists.
func IsExist(username string) (*model.User, error) {
	var user model.User
	if err := repo.Db.Model(&model.User{}).Where("user_name = ?", username).First(&user).Error; err != nil {
		return &model.User{}, err
	}
	return &user, nil
}

// CheckPassword verifies a user's password.
func CheckPassword(username, password string) error {
	var user model.User
	if err := repo.Db.Model(&model.User{}).Where("user_name = ?", username).First(&user).Error; err != nil {
		return err
	}
	if !utils.CheckPwd(password, user.Password) {
		return errors.New("password error")
	}
	return nil
}

// IsEmailExist checks whether an email exists.
func IsEmailExist(email string) error {
	var user model.User
	if err := repo.Db.Model(&model.User{}).Where("email = ?", email).First(&user).Error; err != nil {
		return err
	}
	return nil
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

// AddFavorite creates a favorite entry for one file/folder.
func AddFavorite(userID, fileID uint64) error {
	if !CheckFileOwner(userID, fileID) {
		return gorm.ErrRecordNotFound
	}

	entry := &model.UserFavorite{
		UserID: userID,
		FileID: fileID,
	}
	return repo.Db.Clauses(clause.OnConflict{DoNothing: true}).Create(entry).Error
}

// RemoveFavorite removes one favorite entry.
func RemoveFavorite(userID, fileID uint64) error {
	return repo.Db.Where("user_id = ? AND file_id = ?", userID, fileID).Delete(&model.UserFavorite{}).Error
}

// ListFavorites lists favorites for one user.
func ListFavorites(userID uint64, limit int) ([]FavoriteItem, error) {
	if limit <= 0 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}

	items := make([]FavoriteItem, 0)
	err := repo.Db.Table("user_favorite uf").
		Select("uf.file_id, f.name, f.is_dir, f.size, f.parent_id, uf.created_at, f.updated_at").
		Joins("JOIN user_file f ON f.id = uf.file_id").
		Where("uf.user_id = ? AND f.is_deleted = 0", userID).
		Order("uf.created_at DESC").
		Limit(limit).
		Scan(&items).Error
	return items, err
}

// RecordRecentAccess upserts one recent access item.
func RecordRecentAccess(userID, fileID uint64, source string) error {
	if userID == 0 || fileID == 0 {
		return nil
	}
	if !CheckFileOwner(userID, fileID) {
		return nil
	}

	now := time.Now()
	src := normalizeRecentSource(source)

	entry := &model.UserRecent{
		UserID:       userID,
		FileID:       fileID,
		Source:       src,
		AccessCount:  1,
		LastAccessAt: now,
	}
	return repo.Db.Clauses(clause.OnConflict{
		Columns: []clause.Column{
			{Name: "user_id"},
			{Name: "file_id"},
		},
		DoUpdates: clause.Assignments(map[string]interface{}{
			"source":         src,
			"last_access_at": now,
			"access_count":   gorm.Expr("access_count + ?", 1),
		}),
	}).Create(entry).Error
}

// ListRecent returns recent access records for one user.
func ListRecent(userID uint64, limit int) ([]RecentItem, error) {
	if limit <= 0 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}

	items := make([]RecentItem, 0)
	err := repo.Db.Table("user_recent ur").
		Select("ur.file_id, f.name, f.is_dir, f.size, f.parent_id, ur.source, ur.access_count, ur.last_access_at").
		Joins("JOIN user_file f ON f.id = ur.file_id").
		Where("ur.user_id = ? AND f.is_deleted = 0", userID).
		Order("ur.last_access_at DESC").
		Limit(limit).
		Scan(&items).Error
	return items, err
}

// ListCommonDirs lists frequently visited directories based on recent accesses.
func ListCommonDirs(userID uint64, limit int) ([]CommonDirItem, error) {
	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}

	items := make([]CommonDirItem, 0)
	err := repo.Db.Table("user_recent ur").
		Select("ur.file_id, f.name, f.parent_id, ur.access_count, ur.last_access_at").
		Joins("JOIN user_file f ON f.id = ur.file_id").
		Where("ur.user_id = ? AND f.is_deleted = 0 AND f.is_dir = 1", userID).
		Order("ur.access_count DESC, ur.last_access_at DESC").
		Limit(limit).
		Scan(&items).Error
	return items, err
}

func normalizeRecentSource(source string) string {
	source = strings.ToLower(strings.TrimSpace(source))
	if source == "" {
		return "unknown"
	}
	if len(source) > 64 {
		return source[:64]
	}
	return source
}
