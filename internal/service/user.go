package service

import (
	"Go_Pan/internal/repo"
	"Go_Pan/model"
	"Go_Pan/utils"
	"context"
	"errors"
	"time"
)

const userInfoCacheTTL = 5 * time.Minute

// CreateUser hashes password and creates a user.
func CreateUser(user *model.User) error {
	// 对密码进行加
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
	// 使用 bcrypt 验证密码
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
