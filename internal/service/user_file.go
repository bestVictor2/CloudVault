package service

import (
	"Go_Pan/internal/dto"
	"Go_Pan/internal/repo"
	"Go_Pan/model"
	"Go_Pan/utils"
	"context"
	"errors"
	"fmt"
	"time"
)

// CreateUserFileEntry creates a file or folder entry.
func CreateUserFileEntry(userFile *model.UserFile) error {
	if userFile.IsDir {
		return CreateUserDir(userFile)
	} else {
		return CreateUserFile(userFile)
	}
}

// cacheParentID normalizes parent ID for cache keys.
func cacheParentID(parentID *uint64) uint64 {
	if parentID == nil {
		return 0
	}
	return *parentID
}

// invalidateFileListCache clears file list cache.
func invalidateFileListCache(userID uint64, parentID *uint64) {
	_ = utils.InvalidateUserFileListCache(context.Background(), userID, cacheParentID(parentID))
}

// getFileParentID returns a file's parent ID.
func getFileParentID(userID, fileID uint64) (*uint64, error) {
	var file model.UserFile
	if err := repo.Db.Unscoped().Where("id = ? AND user_id = ?", fileID, userID).First(&file).Error; err != nil {
		return nil, err
	}
	return file.ParentID, nil
}

// CreateUserFile creates a file entry.
func CreateUserFile(userFile *model.UserFile) error {
	if userFile.ObjectID == nil {
		return fmt.Errorf("file must have objectId")
	}
	file := &model.UserFile{
		UserID:   userFile.UserID,
		ParentID: userFile.ParentID,
		Name:     userFile.Name,
		IsDir:    false,
		ObjectID: userFile.ObjectID,
		Size:     userFile.Size,
	}
	if err := repo.Db.Create(file).Error; err != nil {
		return err
	}
	// 更新传入对象的ID
	userFile.ID = file.ID
	invalidateFileListCache(userFile.UserID, userFile.ParentID)
	return nil
}

// CreateUserDir creates a folder entry.
func CreateUserDir(userFile *model.UserFile) error {
	if userFile.ParentID != nil && *userFile.ParentID != 0 {
		var parent model.UserFile
		if err := repo.Db.
			Where("id = ? AND user_id = ? AND is_dir = 1 AND is_deleted = 0",
				userFile.ParentID, userFile.UserID).
			First(&parent).Error; err != nil {
			return fmt.Errorf("parent not exist or not dir")
		}
	}
	dir := &model.UserFile{
		UserID:   userFile.UserID,
		ParentID: userFile.ParentID,
		Name:     userFile.Name,
		IsDir:    true,
		ObjectID: nil,
		Size:     0,
	}
	if err := repo.Db.Create(dir).Error; err != nil {
		return err
	}
	// 更新传入对象的ID
	userFile.ID = dir.ID
	invalidateFileListCache(userFile.UserID, userFile.ParentID)
	return nil
}

// GetDeletedFile returns a deleted file record.
func GetDeletedFile(userID, fileID uint) (*model.UserFile, error) { // 查询单个文件
	var file model.UserFile
	err := repo.Db.
		Unscoped().
		Where("id = ? AND user_id = ? AND is_deleted = 1", fileID, userID).
		First(&file).Error
	return &file, err
}

// MoveToRecycle moves a file to the recycle bin.
func MoveToRecycle(userId, fileId uint64) error {
	now := time.Now()
	parentID, err := getFileParentID(userId, fileId)
	if err != nil {
		return err
	}
	if err := repo.Db.Model(&model.UserFile{}).
		Where("id = ? AND user_id = ? AND is_deleted = 0", fileId, userId).
		Updates(map[string]interface{}{
			"is_deleted": true,
			"deleted_at": &now,
		}).Error; err != nil {
		return err
	}
	invalidateFileListCache(userId, parentID)
	return nil
}

// ListRecycleFiles lists recycle bin files.
func ListRecycleFiles(userID uint) ([]model.UserFile, error) {
	var files []model.UserFile
	err := repo.Db.
		Unscoped().
		Where("user_id = ? AND is_deleted = 1", userID).
		Order("deleted_at DESC").
		Find(&files).Error
	return files, err
}

// RestoreFile restores a recycled file.
func RestoreFile(userID, fileID uint) error { // 恢复文件
	parentID, err := getFileParentID(uint64(userID), uint64(fileID))
	if err != nil {
		return err
	}
	if err := repo.Db.Model(&model.UserFile{}).
		Unscoped().
		Where("id = ? AND user_id = ? AND is_deleted = 1", fileID, userID).
		Updates(map[string]interface{}{
			"is_deleted": false,
			"deleted_at": nil,
		}).Error; err != nil {
		return err
	}
	invalidateFileListCache(uint64(userID), parentID)
	return nil
}

// DeleteFileRecord permanently deletes a file record.
func DeleteFileRecord(userID, fileID uint) error {
	file, err := GetDeletedFile(userID, fileID)
	if err != nil {
		return errors.New("file not found")
	}

	if file.IsDir {
		if err := deleteFolderRecursively(file.ID); err != nil {
			return err
		}
	} else {
		if err := repo.Db.Unscoped().Delete(&model.UserFile{}, fileID).Error; err != nil {
			return err
		}
		if file.ObjectID != nil {
			if err := RemoveObject(*file.ObjectID); err != nil {
				return err
			}
		}
	}

	if file.IsDir {
		if err := repo.Db.Unscoped().Delete(&model.UserFile{}, fileID).Error; err != nil {
			return err
		}
	}
	invalidateFileListCache(uint64(userID), file.ParentID)
	return nil
}

// deleteFolderRecursively 递归删除文件夹及其所有子文件
func deleteFolderRecursively(folderID uint64) error {
	// 查找所有子文件
	var children []model.UserFile
	if err := repo.Db.Unscoped().Where("parent_id = ?", folderID).Find(&children).Error; err != nil {
		return err
	}

	// 递归删除每个子文
	for _, child := range children {
		if child.IsDir {

			if err := deleteFolderRecursively(child.ID); err != nil {
				return err
			}

			if err := repo.Db.Unscoped().Delete(&model.UserFile{}, child.ID).Error; err != nil {
				return err
			}
		} else {

			if err := repo.Db.Unscoped().Delete(&model.UserFile{}, child.ID).Error; err != nil {
				return err
			}
			if child.ObjectID != nil {
				if err := RemoveObject(*child.ObjectID); err != nil {
					return err
				}
			}
		}
	}

	return nil
}

// CheckFileOwner checks file ownership.
func CheckFileOwner(userID, fileID uint64) bool { // 判断该文件是否属于该用户
	var count int64

	err := repo.Db.
		Model(&model.UserFile{}).
		Where("id = ? AND user_id = ? AND is_deleted = 0", fileID, userID).
		Count(&count).Error

	if err != nil {
		return false
	}

	return count > 0
}

// GetUserFileById returns a file by ID.
func GetUserFileById(fileID uint64) (*model.UserFile, error) {
	var file model.UserFile
	err := repo.Db.Where("id = ? AND is_deleted = 0", fileID).First(&file).Error
	return &file, err
}

// GetUserFileByObjectID returns a user's file by object ID.
func GetUserFileByObjectID(userID, objectID uint64) (*model.UserFile, error) {
	var file model.UserFile
	err := repo.Db.
		Where("user_id = ? AND object_id = ? AND is_deleted = 0 AND is_dir = 0", userID, objectID).
		First(&file).Error
	return &file, err
}

// GetFileList 获取文件列表(支持分页、排序)
func GetFileList(userID uint64, req *dto.FileListRequest) ([]model.UserFile, int64, error) {
	var files []model.UserFile
	var total int64

	parentID := uint64(0)
	if req.ParentID != nil {
		parentID = *req.ParentID
	}

	if cached, ok := utils.GetUserFileListFromCache(
		context.Background(),
		userID,
		parentID,
		req.Page,
		req.PageSize,
		req.OrderBy,
		req.OrderDesc,
	); ok {
		return cached.Files, cached.Total, nil
	}

	query := repo.Db.Model(&model.UserFile{}).
		Where("user_id = ? AND is_deleted = 0", userID)

	if req.ParentID == nil || *req.ParentID == 0 {
		query = query.Where("parent_id IS NULL")
	} else {
		query = query.Where("parent_id = ?", *req.ParentID)
	}

	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	order := "is_dir DESC"
	if orderBy := sanitizeOrderBy(req.OrderBy); orderBy != "" {
		if req.OrderDesc {
			order += ", " + orderBy + " DESC"
		} else {
			order += ", " + orderBy + " ASC"
		}
	} else {
		order += ", created_at DESC"
	}

	offset := (req.Page - 1) * req.PageSize
	if err := query.Order(order).Offset(offset).Limit(req.PageSize).Find(&files).Error; err != nil {
		return nil, 0, err
	}

	_ = utils.SetUserFileListToCache(
		context.Background(),
		userID,
		parentID,
		req.Page,
		req.PageSize,
		req.OrderBy,
		req.OrderDesc,
		&utils.FileListCache{Files: files, Total: total},
		2*time.Minute,
	)

	return files, total, nil
}

// RenameFile 重命名文
func RenameFile(userID, fileID uint64, newName string) error {

	var file model.UserFile
	if err := repo.Db.Where("id = ? AND user_id = ? AND is_deleted = 0", fileID, userID).First(&file).Error; err != nil {
		return fmt.Errorf("file not found")
	}

	var count int64
	if err := repo.Db.Model(&model.UserFile{}).
		Where("user_id = ? AND parent_id = ? AND name = ? AND id != ? AND is_deleted = 0",
			userID, file.ParentID, newName, fileID).
		Count(&count).Error; err != nil {
		return err
	}
	if count > 0 {
		return fmt.Errorf("file with same name already exists")
	}

	if err := repo.Db.Model(&file).Update("name", newName).Error; err != nil {
		return err
	}
	invalidateFileListCache(userID, file.ParentID)
	return nil
}

// MoveFiles 移动文件(支持批量)
func MoveFiles(userID uint64, fileIDs []uint64, targetID *uint64) error {
	if targetID != nil && *targetID != 0 {
		var target model.UserFile
		if err := repo.Db.Where("id = ? AND user_id = ? AND is_dir = 1 AND is_deleted = 0", *targetID, userID).First(&target).Error; err != nil {
			return fmt.Errorf("target folder not found")
		}

		for _, fileID := range fileIDs {
			if *targetID == fileID {
				return fmt.Errorf("cannot move folder to itself")
			}
			// 检查目标是否为当前文件的子目录
			if isChildFolder(userID, *targetID, fileID) {
				return fmt.Errorf("cannot move folder to its subfolder")
			}
		}
	}

	var files []model.UserFile
	if err := repo.Db.Where("id IN ? AND user_id = ? AND is_deleted = 0", fileIDs, userID).Find(&files).Error; err != nil {
		return err
	}

	if len(files) != len(fileIDs) {
		return fmt.Errorf("some files not found")
	}

	for _, file := range files {
		var count int64
		query := repo.Db.Model(&model.UserFile{}).
			Where("user_id = ? AND name = ? AND is_deleted = 0", userID, file.Name)
		if targetID == nil || *targetID == 0 {
			query = query.Where("parent_id IS NULL")
		} else {
			query = query.Where("parent_id = ?", *targetID)
		}
		if err := query.Count(&count).Error; err != nil {
			return err
		}
		if count > 0 {
			return fmt.Errorf("file %s already exists in target folder", file.Name)
		}
	}

	if err := repo.Db.Model(&model.UserFile{}).
		Where("id IN ? AND user_id = ? AND is_deleted = 0", fileIDs, userID).
		Update("parent_id", targetID).Error; err != nil {
		return err
	}

	parentIDs := make(map[uint64]struct{})
	for _, file := range files {
		parentIDs[cacheParentID(file.ParentID)] = struct{}{}
	}
	for id := range parentIDs {
		pid := id
		invalidateFileListCache(userID, &pid)
	}
	if targetID != nil {
		invalidateFileListCache(userID, targetID)
	} else {
		root := uint64(0)
		invalidateFileListCache(userID, &root)
	}
	return nil
}

// isChildFolder 检查folderID是否是parentID的子文件
func isChildFolder(userID, folderID, parentID uint64) bool {
	var child model.UserFile
	if err := repo.Db.Where("id = ? AND user_id = ? AND is_dir = 1", folderID, userID).First(&child).Error; err != nil {
		return false
	}

	// 递归查找
	currentParent := child.ParentID
	for currentParent != nil {
		if *currentParent == parentID {
			return true
		}
		var parent model.UserFile
		if err := repo.Db.Where("id = ? AND user_id = ?", *currentParent, userID).First(&parent).Error; err != nil {
			return false
		}
		currentParent = parent.ParentID
	}
	return false
}

// CopyFiles 复制文件(支持批量)
func CopyFiles(userID uint64, fileIDs []uint64, targetID *uint64) error {

	if targetID != nil && *targetID != 0 {
		var target model.UserFile
		if err := repo.Db.Where("id = ? AND user_id = ? AND is_dir = 1 AND is_deleted = 0", *targetID, userID).First(&target).Error; err != nil {
			return fmt.Errorf("target folder not found")
		}
	}

	var files []model.UserFile
	if err := repo.Db.Where("id IN ? AND user_id = ? AND is_deleted = 0", fileIDs, userID).Find(&files).Error; err != nil {
		return err
	}

	if len(files) != len(fileIDs) {
		return fmt.Errorf("some files not found")
	}

	// 逐个复制文件
	for _, file := range files {
		// 检查目标目录是否有重名
		var count int64
		query := repo.Db.Model(&model.UserFile{}).
			Where("user_id = ? AND name = ? AND is_deleted = 0", userID, file.Name)
		if targetID == nil || *targetID == 0 {
			query = query.Where("parent_id IS NULL")
		} else {
			query = query.Where("parent_id = ?", *targetID)
		}
		if err := query.Count(&count).Error; err != nil {
			return err
		}
		if count > 0 {
			return fmt.Errorf("file %s already exists in target folder", file.Name)
		}

		// 创建新的文件记录
		newFile := &model.UserFile{
			UserID:   userID,
			ParentID: targetID,
			Name:     file.Name,
			IsDir:    file.IsDir,
			ObjectID: file.ObjectID,
			Size:     file.Size,
		}

		if err := repo.Db.Create(newFile).Error; err != nil {
			return err
		}

		// 若为文件夹，递归复制子文
		if file.IsDir {
			var children []model.UserFile
			if err := repo.Db.Where("parent_id = ? AND is_deleted = 0", file.ID).Find(&children).Error; err != nil {
				return err
			}

			childIDs := make([]uint64, 0, len(children))
			for _, child := range children {
				childIDs = append(childIDs, child.ID)
			}

			if len(childIDs) > 0 {
				newParentID := newFile.ID
				if err := CopyFiles(userID, childIDs, &newParentID); err != nil {
					return err
				}
			}
		}

		// 对象引用计数加一
		if file.ObjectID != nil {
			if err := IncreaseRefCount(*file.ObjectID); err != nil {
				return err
			}
		}
	}

	if targetID != nil {
		invalidateFileListCache(userID, targetID)
	} else {
		root := uint64(0)
		invalidateFileListCache(userID, &root)
	}
	return nil
}

// BatchMoveToRecycle 批量移入回收
func BatchMoveToRecycle(userID uint64, fileIDs []uint64) error {
	now := time.Now()
	var files []model.UserFile
	if err := repo.Db.Where("id IN ? AND user_id = ? AND is_deleted = 0", fileIDs, userID).Find(&files).Error; err != nil {
		return err
	}
	if err := repo.Db.Model(&model.UserFile{}).
		Where("id IN ? AND user_id = ? AND is_deleted = 0", fileIDs, userID).
		Updates(map[string]interface{}{
			"is_deleted": true,
			"deleted_at": &now,
		}).Error; err != nil {
		return err
	}
	parentIDs := make(map[uint64]struct{})
	for _, file := range files {
		parentIDs[cacheParentID(file.ParentID)] = struct{}{}
	}
	for id := range parentIDs {
		pid := id
		invalidateFileListCache(userID, &pid)
	}
	return nil
}

// CreateFolder 创建文件夹（支持嵌套）
func CreateFolder(userID uint64, parentID *uint64, name string) error {
	// 检查父文件夹是否存在
	if parentID != nil && *parentID != 0 {
		var parent model.UserFile
		if err := repo.Db.Where("id = ? AND user_id = ? AND is_dir = 1 AND is_deleted = 0", *parentID, userID).First(&parent).Error; err != nil {
			return fmt.Errorf("parent folder not found")
		}
	}

	// 检查同名文件夹是否已存在
	var count int64
	query := repo.Db.Model(&model.UserFile{}).
		Where("user_id = ? AND name = ? AND is_dir = 1 AND is_deleted = 0", userID, name)
	if parentID == nil || *parentID == 0 {
		query = query.Where("parent_id IS NULL")
	} else {
		query = query.Where("parent_id = ?", *parentID)
	}
	if err := query.Count(&count).Error; err != nil {
		return err
	}
	if count > 0 {
		return fmt.Errorf("folder already exists")
	}

	// 创建文件夹
	dir := &model.UserFile{
		UserID:   userID,
		ParentID: parentID,
		Name:     name,
		IsDir:    true,
		ObjectID: nil,
		Size:     0,
	}
	if err := repo.Db.Create(dir).Error; err != nil {
		return err
	}
	invalidateFileListCache(userID, parentID)
	return nil
}
