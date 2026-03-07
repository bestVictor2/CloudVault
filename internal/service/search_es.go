package service

import (
	"CloudVault/config"
	"CloudVault/internal/dto"
	"CloudVault/internal/repo"
	"CloudVault/internal/storage"
	"CloudVault/model"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"gorm.io/gorm"
)

type esUserFileDoc struct {
	FileID    uint64    `json:"file_id"`
	UserID    uint64    `json:"user_id"`
	ParentID  uint64    `json:"parent_id"`
	Name      string    `json:"name"`
	Path      string    `json:"path"`
	Content   string    `json:"content,omitempty"`
	IsDir     bool      `json:"is_dir"`
	IsDeleted bool      `json:"is_deleted"`
	Size      int64     `json:"size"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type esSearchResponse struct {
	Hits struct {
		Total json.RawMessage `json:"total"`
		Hits  []struct {
			ID string `json:"_id"`
		} `json:"hits"`
	} `json:"hits"`
}

var (
	esIndexReady    atomic.Bool
	esBackfillUsers sync.Map
)

func isESSearchEnabled() bool {
	return config.AppConfig.ESEnabled &&
		strings.TrimSpace(config.AppConfig.ESAddress) != "" &&
		strings.TrimSpace(config.AppConfig.ESIndex) != ""
}

// EnsureUserFilesSearchIndexed performs a one-time backfill for a user in this process lifetime.
func EnsureUserFilesSearchIndexed(ctx context.Context, userID uint64) error {
	if !isESSearchEnabled() || userID == 0 {
		return nil
	}
	if _, loaded := esBackfillUsers.LoadOrStore(userID, struct{}{}); loaded {
		return nil
	}
	if err := backfillUserFilesToES(ctx, userID); err != nil {
		esBackfillUsers.Delete(userID)
		return err
	}
	return nil
}

func backfillUserFilesToES(ctx context.Context, userID uint64) error {
	const batchSize = 200
	var lastID uint64
	for {
		var files []model.UserFile
		query := repo.Db.
			Where("user_id = ? AND is_deleted = 0 AND id > ?", userID, lastID).
			Order("id ASC").
			Limit(batchSize)
		if err := query.Find(&files).Error; err != nil {
			return err
		}
		if len(files) == 0 {
			break
		}
		for _, file := range files {
			if err := SyncUserFileSearchIndexByID(ctx, file.ID); err != nil {
				log.Printf("es backfill item failed file_id=%d: %v", file.ID, err)
			}
		}
		lastID = files[len(files)-1].ID
	}
	return nil
}

// SyncUserFileSearchIndexByID upserts a user_file document into Elasticsearch.
func SyncUserFileSearchIndexByID(ctx context.Context, fileID uint64) error {
	if !isESSearchEnabled() || fileID == 0 {
		return nil
	}
	if err := ensureESIndex(ctx); err != nil {
		return err
	}

	var file model.UserFile
	err := repo.Db.Unscoped().Where("id = ?", fileID).First(&file).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return DeleteUserFileSearchIndexByID(ctx, fileID)
	}
	if err != nil {
		return err
	}
	if file.IsDeleted {
		return DeleteUserFileSearchIndexByID(ctx, fileID)
	}

	doc := buildESUserFileDoc(ctx, &file)
	body, err := json.Marshal(doc)
	if err != nil {
		return err
	}
	status, respBody, err := doESRequest(
		ctx,
		http.MethodPut,
		"/"+url.PathEscape(config.AppConfig.ESIndex)+"/_doc/"+strconv.FormatUint(fileID, 10),
		body,
	)
	if err != nil {
		return err
	}
	if status >= 300 {
		return fmt.Errorf("es upsert failed: status=%d body=%s", status, compactErrorText(respBody))
	}
	return nil
}

// DeleteUserFileSearchIndexByID removes a user_file document from Elasticsearch.
func DeleteUserFileSearchIndexByID(ctx context.Context, fileID uint64) error {
	if !isESSearchEnabled() || fileID == 0 {
		return nil
	}
	if err := ensureESIndex(ctx); err != nil {
		return err
	}
	status, respBody, err := doESRequest(
		ctx,
		http.MethodDelete,
		"/"+url.PathEscape(config.AppConfig.ESIndex)+"/_doc/"+strconv.FormatUint(fileID, 10),
		nil,
	)
	if err != nil {
		return err
	}
	if status == http.StatusNotFound {
		return nil
	}
	if status >= 300 {
		return fmt.Errorf("es delete failed: status=%d body=%s", status, compactErrorText(respBody))
	}
	return nil
}

func searchFilesByES(userID uint64, req *dto.FileSearchRequest) ([]model.UserFile, int64, error) {
	if !isESSearchEnabled() {
		return nil, 0, errors.New("es disabled")
	}
	if err := ensureESIndex(nil); err != nil {
		return nil, 0, err
	}

	parentID := uint64(0)
	if req.ParentID != nil && *req.ParentID != 0 {
		parentID = *req.ParentID
	}
	offset := (req.Page - 1) * req.PageSize

	query := map[string]any{
		"from":             offset,
		"size":             req.PageSize,
		"track_total_hits": true,
		"_source":          false,
		"query": map[string]any{
			"bool": map[string]any{
				"must": []map[string]any{
					{
						"multi_match": map[string]any{
							"query":  req.Query,
							"fields": []string{"name^5", "path^2", "content"},
						},
					},
				},
				"filter": []map[string]any{
					{"term": map[string]any{"user_id": userID}},
					{"term": map[string]any{"is_deleted": false}},
					{"term": map[string]any{"parent_id": parentID}},
				},
			},
		},
		"sort": []map[string]any{
			{"_score": map[string]string{"order": "desc"}},
			{"updated_at": map[string]string{"order": "desc"}},
		},
	}
	body, err := json.Marshal(query)
	if err != nil {
		return nil, 0, err
	}
	status, respBody, err := doESRequest(
		nil,
		http.MethodPost,
		"/"+url.PathEscape(config.AppConfig.ESIndex)+"/_search",
		body,
	)
	if err != nil {
		return nil, 0, err
	}
	if status >= 300 {
		return nil, 0, fmt.Errorf("es search failed: status=%d body=%s", status, compactErrorText(respBody))
	}

	var parsed esSearchResponse
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return nil, 0, err
	}

	total := parseESTotalHits(parsed.Hits.Total)
	if len(parsed.Hits.Hits) == 0 {
		return []model.UserFile{}, total, nil
	}
	ids := make([]uint64, 0, len(parsed.Hits.Hits))
	for _, hit := range parsed.Hits.Hits {
		id, err := strconv.ParseUint(hit.ID, 10, 64)
		if err != nil {
			continue
		}
		ids = append(ids, id)
	}
	files, err := loadUserFilesByOrderedIDs(userID, ids)
	if err != nil {
		return nil, 0, err
	}
	return files, total, nil
}

func parseESTotalHits(raw json.RawMessage) int64 {
	if len(raw) == 0 {
		return 0
	}
	var direct int64
	if err := json.Unmarshal(raw, &direct); err == nil {
		return direct
	}
	var wrapped struct {
		Value int64 `json:"value"`
	}
	if err := json.Unmarshal(raw, &wrapped); err == nil {
		return wrapped.Value
	}
	return 0
}

func loadUserFilesByOrderedIDs(userID uint64, ids []uint64) ([]model.UserFile, error) {
	if len(ids) == 0 {
		return []model.UserFile{}, nil
	}
	var files []model.UserFile
	if err := repo.Db.
		Where("id IN ? AND user_id = ? AND is_deleted = 0", ids, userID).
		Find(&files).Error; err != nil {
		return nil, err
	}
	byID := make(map[uint64]model.UserFile, len(files))
	for _, file := range files {
		byID[file.ID] = file
	}
	ordered := make([]model.UserFile, 0, len(files))
	for _, id := range ids {
		file, ok := byID[id]
		if !ok {
			continue
		}
		ordered = append(ordered, file)
	}
	return ordered, nil
}

func buildESUserFileDoc(ctx context.Context, file *model.UserFile) *esUserFileDoc {
	doc := &esUserFileDoc{
		FileID:    file.ID,
		UserID:    file.UserID,
		ParentID:  normalizeParentID(file.ParentID),
		Name:      file.Name,
		Path:      buildUserFilePath(file),
		IsDir:     file.IsDir,
		IsDeleted: file.IsDeleted,
		Size:      file.Size,
		CreatedAt: file.CreatedAt,
		UpdatedAt: file.UpdatedAt,
	}
	if file.IsDir || file.ObjectID == nil {
		return doc
	}
	obj, err := GetFileObjectById(*file.ObjectID)
	if err != nil || obj == nil {
		return doc
	}
	doc.Content = extractTextContentForSearch(ctx, file.Name, obj)
	return doc
}

func normalizeParentID(parentID *uint64) uint64 {
	if parentID == nil {
		return 0
	}
	return *parentID
}

func buildUserFilePath(file *model.UserFile) string {
	if file == nil {
		return "/"
	}
	parts := []string{strings.TrimSpace(file.Name)}
	cur := file.ParentID
	for i := 0; i < 64 && cur != nil && *cur != 0; i++ {
		var parent model.UserFile
		if err := repo.Db.Unscoped().
			Select("id", "name", "parent_id", "user_id").
			Where("id = ? AND user_id = ?", *cur, file.UserID).
			First(&parent).Error; err != nil {
			break
		}
		parts = append(parts, strings.TrimSpace(parent.Name))
		cur = parent.ParentID
	}
	for i, j := 0, len(parts)-1; i < j; i, j = i+1, j-1 {
		parts[i], parts[j] = parts[j], parts[i]
	}
	filtered := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		filtered = append(filtered, part)
	}
	if len(filtered) == 0 {
		return "/"
	}
	return "/" + strings.Join(filtered, "/")
}

func extractTextContentForSearch(ctx context.Context, fileName string, obj *model.FileObject) string {
	if obj == nil || !isTextIndexable(fileName) || storage.Default == nil {
		return ""
	}
	maxBytes := config.AppConfig.ESContentMaxBytes
	if maxBytes <= 0 {
		return ""
	}
	reader, _, err := storage.Default.GetObject(ctxOrBackground(ctx), obj.BucketName, obj.ObjectName)
	if err != nil {
		return ""
	}
	defer reader.Close()

	content, err := io.ReadAll(io.LimitReader(reader, maxBytes))
	if err != nil || len(content) == 0 {
		return ""
	}
	// Probable binary payload.
	if bytes.IndexByte(content, 0) >= 0 {
		return ""
	}
	return strings.TrimSpace(string(bytes.ToValidUTF8(content, nil)))
}

func isTextIndexable(fileName string) bool {
	switch strings.ToLower(path.Ext(fileName)) {
	case ".txt", ".md", ".markdown", ".csv", ".json", ".xml", ".yaml", ".yml",
		".log", ".ini", ".conf", ".toml", ".html", ".htm", ".js", ".ts", ".go",
		".java", ".py", ".rs", ".sql":
		return true
	default:
		return false
	}
}

func ensureESIndex(ctx context.Context) error {
	if esIndexReady.Load() {
		return nil
	}
	index := strings.TrimSpace(config.AppConfig.ESIndex)
	if index == "" {
		return errors.New("es index not configured")
	}
	path := "/" + url.PathEscape(index)

	status, respBody, err := doESRequest(ctx, http.MethodHead, path, nil)
	if err != nil {
		return err
	}
	if status == http.StatusOK {
		esIndexReady.Store(true)
		return nil
	}
	if status != http.StatusNotFound {
		return fmt.Errorf("es index check failed: status=%d body=%s", status, compactErrorText(respBody))
	}

	mapping := map[string]any{
		"mappings": map[string]any{
			"properties": map[string]any{
				"file_id":    map[string]any{"type": "long"},
				"user_id":    map[string]any{"type": "long"},
				"parent_id":  map[string]any{"type": "long"},
				"name":       map[string]any{"type": "text", "fields": map[string]any{"keyword": map[string]any{"type": "keyword", "ignore_above": 256}}},
				"path":       map[string]any{"type": "text"},
				"content":    map[string]any{"type": "text"},
				"is_dir":     map[string]any{"type": "boolean"},
				"is_deleted": map[string]any{"type": "boolean"},
				"size":       map[string]any{"type": "long"},
				"created_at": map[string]any{"type": "date"},
				"updated_at": map[string]any{"type": "date"},
			},
		},
	}
	body, err := json.Marshal(mapping)
	if err != nil {
		return err
	}
	status, respBody, err = doESRequest(ctx, http.MethodPut, path, body)
	if err != nil {
		return err
	}
	if status >= 300 && status != http.StatusBadRequest {
		return fmt.Errorf("es create index failed: status=%d body=%s", status, compactErrorText(respBody))
	}
	esIndexReady.Store(true)
	return nil
}

func doESRequest(ctx context.Context, method, requestPath string, body []byte) (int, []byte, error) {
	if !isESSearchEnabled() {
		return 0, nil, errors.New("es disabled")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	timeout := config.AppConfig.ESTimeout
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	base := strings.TrimRight(config.AppConfig.ESAddress, "/")
	if !strings.HasPrefix(requestPath, "/") {
		requestPath = "/" + requestPath
	}
	fullURL := base + requestPath
	req, err := http.NewRequestWithContext(ctx, method, fullURL, bytes.NewReader(body))
	if err != nil {
		return 0, nil, err
	}
	if len(body) > 0 {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Accept", "application/json")
	if apiKey := strings.TrimSpace(config.AppConfig.ESAPIKey); apiKey != "" {
		req.Header.Set("Authorization", "ApiKey "+apiKey)
	} else if user := strings.TrimSpace(config.AppConfig.ESUsername); user != "" {
		req.SetBasicAuth(user, config.AppConfig.ESPassword)
	}

	client := &http.Client{Timeout: timeout}
	resp, err := client.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return resp.StatusCode, nil, err
	}
	return resp.StatusCode, respBody, nil
}

func ctxOrBackground(ctx context.Context) context.Context {
	if ctx != nil {
		return ctx
	}
	return context.Background()
}
