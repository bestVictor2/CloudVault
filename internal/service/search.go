package service

import (
	"CloudVault/internal/dto"
	"CloudVault/internal/repo"
	"CloudVault/model"
	"fmt"
	"log"
	"strings"
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
			if shouldFallbackToDBByESResult(req, files, total) {
				log.Printf("es search fallback to mysql: inconsistent es result total=%d rows=%d page=%d page_size=%d", total, len(files), req.Page, req.PageSize)
				return searchFilesByDB(userID, req)
			}
			return files, total, nil
		}
		log.Printf("es search fallback to mysql: %v", err)
	}
	return searchFilesByDB(userID, req)
}

func shouldFallbackToDBByESResult(req *dto.FileSearchRequest, files []model.UserFile, total int64) bool {
	if req == nil {
		return false
	}
	if total < 0 {
		return true
	}
	// Covers delayed/dirty ES index where total says hit exists but hydrated DB rows are empty.
	if total > 0 && len(files) == 0 {
		return true
	}
	// For first page, if ES total is small enough to fit in one page but returned fewer rows,
	// prefer MySQL for correctness.
	if req.Page <= 1 && req.PageSize > 0 && total > int64(len(files)) && total <= int64(req.PageSize) {
		return true
	}
	// If ES reports no results, verify once via DB (safe and cheap for page 1).
	if req.Page <= 1 && total == 0 {
		return true
	}
	return false
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

var allowedOrderBy = map[string]string{
	"created_at": "created_at",
	"updated_at": "updated_at",
	"name":       "name",
	"size":       "size",
	"id":         "id",
}

func sanitizeOrderBy(orderBy string) string {
	key := strings.ToLower(strings.TrimSpace(orderBy))
	return allowedOrderBy[key]
}
