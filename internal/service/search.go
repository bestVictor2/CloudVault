package service

import (
	"CloudVault/internal/dto"
	"CloudVault/internal/repo"
	"CloudVault/model"
	"fmt"
	"log"
)

// SearchFiles searches files by keyword.
// Priority:
// 1) Elasticsearch (when enabled)
// 2) MySQL LIKE fallback
func SearchFiles(userID uint64, req *dto.FileSearchRequest) ([]model.UserFile, int64, error) {
	if isESSearchEnabled() {
		if err := EnsureUserFilesSearchIndexed(nil, userID); err != nil {
			log.Printf("es backfill skipped: %v", err)
		}
		files, total, err := searchFilesByES(userID, req)
		if err == nil {
			return files, total, nil
		}
		log.Printf("es search fallback to mysql: %v", err)
	}
	return searchFilesByDB(userID, req)
}

func searchFilesByDB(userID uint64, req *dto.FileSearchRequest) ([]model.UserFile, int64, error) {
	var files []model.UserFile
	var total int64

	query := repo.Db.Model(&model.UserFile{}).
		Where("user_id = ? AND is_deleted = 0", userID).
		Where("name LIKE ?", fmt.Sprintf("%%%s%%", req.Query))

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
	return files, total, nil
}
