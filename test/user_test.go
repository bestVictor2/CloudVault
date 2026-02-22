package test

import (
	"CloudVault/internal/repo"
	"CloudVault/internal/service"
	"CloudVault/model"
	"testing"
)

// cleanUserTable clears user table for tests.
func cleanUserTable(t *testing.T) {
	// 临时禁用外键检
	repo.Db.Exec("SET FOREIGN_KEY_CHECKS = 0")

	// 按照外键依赖关系的顺序清理表数据
	tables := []string{"file_share", "file_chunk", "upload_session", "user_file", "file_object", "user_db"}
	for _, table := range tables {
		if err := repo.Db.Exec("DELETE FROM " + table).Error; err != nil {
			t.Fatalf("clean %s table failed: %v", table, err)
		}
	}

	// 重新启用外键检
	repo.Db.Exec("SET FOREIGN_KEY_CHECKS = 1")
}

// TestCreateUser tests user creation.
func TestCreateUser(t *testing.T) {
	cleanUserTable(t)

	user := &model.User{
		UserName: "test_create",
		Password: "123456",
		Email:    "create@test.com",
	}

	if err := service.CreateUser(user); err != nil {
		t.Fatalf("CreateUser failed: %v", err)
	}

	if user.ID == 0 {
		t.Fatal("user ID should not be zero after create")
	}
}

// TestFindIdByUsername tests lookup by username.
func TestFindIdByUsername(t *testing.T) {
	cleanUserTable(t)

	user := &model.User{
		UserName: "find_id",
		Password: "123",
		Email:    "findid@test.com",
	}
	_ = service.CreateUser(user)

	_, err := service.FindIdByUsername("find_id")
	if err != nil {
		t.Fatalf("FindIdByUsername failed: %v", err)
	}

}

// TestFindUserNameById tests lookup by ID.
func TestFindUserNameById(t *testing.T) {
	cleanUserTable(t)

	user := &model.User{
		UserName: "find_name",
		Password: "123",
		Email:    "findname@test.com",
	}
	_ = service.CreateUser(user)

	name, err := service.FindUserNameById(user.ID)
	if err != nil {
		t.Fatalf("FindUserNameById failed: %v", err)
	}

	if name != user.UserName {
		t.Fatalf("expect %s, got %s", user.UserName, name)
	}
}

// TestIsExist tests user existence.
func TestIsExist(t *testing.T) {
	cleanUserTable(t)

	user := &model.User{
		UserName: "exist_user",
		Password: "123",
		Email:    "exist@test.com",
	}
	_ = service.CreateUser(user)

	u, err := service.IsExist("exist_user")
	if err != nil {
		t.Fatalf("IsExist failed: %v", err)
	}
	if u.UserName != "exist_user" {
		t.Fatalf("expect exist_user, got %s", u.UserName)
	}
}

// TestCheckPassword tests password verification.
func TestCheckPassword(t *testing.T) {
	cleanUserTable(t)

	user := &model.User{
		UserName: "pwd_user",
		Password: "correct_pwd",
		Email:    "pwd@test.com",
	}
	_ = service.CreateUser(user)

	if err := service.CheckPassword("pwd_user", "correct_pwd"); err != nil {
		t.Fatalf("CheckPassword should success, got err: %v", err)
	}

	if err := service.CheckPassword("pwd_user", "wrong_pwd"); err == nil {
		t.Fatalf("CheckPassword should fail with wrong password")
	}
}

// TestIsEmailExist tests email existence.
func TestIsEmailExist(t *testing.T) {
	cleanUserTable(t)

	user := &model.User{
		UserName: "email_user",
		Password: "123",
		Email:    "email@test.com",
	}
	_ = service.CreateUser(user)

	if err := service.IsEmailExist("email@test.com"); err != nil {
		t.Fatalf("IsEmailExist should return nil")
	}

	if err := service.IsEmailExist("not_exist@test.com"); err == nil {
		t.Fatalf("IsEmailExist should fail for non-exist email")
	}
}



