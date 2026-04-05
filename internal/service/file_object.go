package service

import (
	"CloudVault/config"
	"CloudVault/internal/dto"
	"CloudVault/internal/repo"
	"CloudVault/internal/storage"
	"CloudVault/model"
	"CloudVault/utils"
	"bytes"
	"errors"
	"fmt"
	"strings"
	"time"

	"golang.org/x/net/context"
	"gorm.io/gorm"
)

const fileObjectCacheTTL = 5 * time.Minute

func cacheFileObject(ctx context.Context, obj *model.FileObject) {
	if obj == nil {
		return
	}
	if ctx == nil {
		ctx = context.Background()
	}
	_ = utils.SetFileObjectToCache(ctx, obj.ID, obj, fileObjectCacheTTL)
	if obj.Hash != "" {
		_ = utils.SetFileObjectIDByHash(ctx, obj.Hash, obj.ID, fileObjectCacheTTL)
	}
	if obj.BucketName != "" && obj.ObjectName != "" {
		_ = utils.SetFileObjectIDByPath(ctx, obj.BucketName, obj.ObjectName, obj.ID, fileObjectCacheTTL)
	}
}

// BuildObjectName builds a content-addressed object path from hash.
func BuildObjectName(hash string) string {
	hash = strings.TrimSpace(strings.ToLower(hash))
	hash = strings.ReplaceAll(hash, "/", "_")
	hash = strings.ReplaceAll(hash, "\\", "_")
	if len(hash) >= 4 {
		return fmt.Sprintf("files/sha256/%s/%s/%s", hash[:2], hash[2:4], hash)
	}
	return fmt.Sprintf("files/sha256/%s", hash)
} // minio 瀛樺偍璺緞

// BuildTempObjectName builds a temporary object path for uploads.
func BuildTempObjectName(username, token string) string {
	return fmt.Sprintf("files/%s/tmp/%s", username, token)
}

// FinalizeUploadedObject deduplicates by hash and creates a user file entry.
// Returns user file ID and whether an existing object was reused.
func FinalizeUploadedObject(
	ctx context.Context,
	userID uint64,
	parentID *uint64,
	fileName string,
	size int64,
	bucketName string,
	objectName string,
	hash string,
) (uint64, bool, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	existingObj, err := GetFileObjectByHash(hash)
	if err == nil {
		available, checkErr := isFileObjectAvailable(ctx, existingObj)
		if checkErr != nil {
			return 0, false, checkErr
		}
		var (
			oldBucket   string
			oldObject   string
			updatedPath bool
			userFile    *model.UserFile
		)
		if !available {
			oldBucket = existingObj.BucketName
			oldObject = existingObj.ObjectName
			updatedPath = true
		} else if existingObj.BucketName != bucketName || existingObj.ObjectName != objectName {
			if storage.Default != nil {
				_ = storage.Default.RemoveObject(ctx, bucketName, objectName)
			}
		}

		if err := repo.Db.Transaction(func(tx *gorm.DB) error {
			if updatedPath {
				if err := tx.Model(&model.FileObject{}).
					Where("id = ?", existingObj.ID).
					Updates(map[string]interface{}{
						"bucket_name": bucketName,
						"object_name": objectName,
						"size":        size,
					}).Error; err != nil {
					return err
				}
				existingObj.BucketName = bucketName
				existingObj.ObjectName = objectName
				existingObj.Size = size
			}
			if err := increaseRefCountTx(tx, existingObj.ID); err != nil {
				return err
			}
			userFile = &model.UserFile{
				UserID:   userID,
				ParentID: parentID,
				Name:     fileName,
				ObjectID: &existingObj.ID,
				Size:     size,
				IsDir:    false,
			}
			return createUserFileEntryTx(tx, userFile)
		}); err != nil {
			return 0, false, err
		}
		if updatedPath && (oldBucket != existingObj.BucketName || oldObject != existingObj.ObjectName) {
			_ = utils.InvalidateFileObjectPathCache(ctx, oldBucket, oldObject)
		}
		_ = utils.InvalidateFileObjectCache(ctx, existingObj.ID)
		cacheFileObject(ctx, existingObj)
		finalizeUserFileEntry(userFile)
		return userFile.ID, true, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return 0, false, err
	}

	obj := &model.FileObject{
		BucketName: bucketName,
		Hash:       hash,
		ObjectName: objectName,
		Size:       size,
		RefCount:   1,
	}
	userFile := &model.UserFile{
		UserID:   userID,
		ParentID: parentID,
		Name:     fileName,
		Size:     size,
		IsDir:    false,
	}
	if err := repo.Db.Transaction(func(tx *gorm.DB) error {
		if err := createFilesObjectTx(tx, obj); err != nil {
			return err
		}
		userFile.ObjectID = &obj.ID
		return createUserFileEntryTx(tx, userFile)
	}); err != nil {
		if storage.Default != nil {
			_ = storage.Default.RemoveObject(ctx, bucketName, objectName)
		}
		return 0, false, err
	}
	cacheFileObject(ctx, obj)
	finalizeUserFileEntry(userFile)
	return userFile.ID, false, nil
}

// DeleteMinioFile removes an object from MinIO.
// MinIO 鍒犻櫎瀵硅薄
func DeleteMinioFile(fileObject *model.FileObject) error {
	ctx := context.Background()
	if storage.Default == nil {
		return fmt.Errorf("storage not initialized")
	}
	return storage.Default.RemoveObject(
		ctx,
		fileObject.BucketName,
		fileObject.ObjectName,
	)
}

// CreateFilesObject inserts a file object record.
func CreateFilesObject(dir *model.FileObject) error {
	normalizeFileObjectForCreate(dir)
	if err := repo.Db.Model(&model.FileObject{}).Create(dir).Error; err != nil {
		return err
	}
	cacheFileObject(context.Background(), dir)
	setRefCountCache(context.Background(), dir.ID, int64(dir.RefCount))
	return nil
}

func createFilesObjectTx(tx *gorm.DB, obj *model.FileObject) error {
	normalizeFileObjectForCreate(obj)
	return tx.Model(&model.FileObject{}).Create(obj).Error
}

func normalizeFileObjectForCreate(obj *model.FileObject) {
	if obj == nil {
		return
	}
	if strings.TrimSpace(obj.DeleteStatus) == "" {
		obj.DeleteStatus = model.FileObjectDeleteStatusActive
	}
	if obj.DeleteStatus == model.FileObjectDeleteStatusActive {
		obj.DeleteAfter = nil
	}
}

func increaseRefCountTx(tx *gorm.DB, id uint64) error {
	_, err := adjustFileObjectRefCount(context.Background(), tx, id, 1)
	return err
}

// GetFileByObject finds a file object by bucket and name.
func GetFileByObject(bucket, object string) (*model.FileObject, error) {
	if id, ok := utils.GetFileObjectIDByPath(context.Background(), bucket, object); ok {
		file, err := GetFileObjectById(id)
		if err == nil {
			return file, nil
		}
		if errors.Is(err, gorm.ErrRecordNotFound) { // 鏁版嵁搴撲腑涓嶅瓨鍦ㄤ絾鏄紦瀛樹腑瀛樺湪 澶勭悊鑴忔暟鎹?			_ = utils.InvalidateFileObjectPathCache(context.Background(), bucket, object)
		} else {
			return nil, err
		}
	}
	var file model.FileObject
	err := repo.Db.Where(
		"bucket_name = ? AND object_name = ?",
		bucket, object,
	).First(&file).Error
	if err == nil {
		cacheFileObject(context.Background(), &file)
	}
	return &file, err
}

// GetFileObjectByHash finds a file object by hash.
func GetFileObjectByHash(hash string) (*model.FileObject, error) {
	if id, ok := utils.GetFileObjectIDByHash(context.Background(), hash); ok {
		obj, err := GetFileObjectById(id)
		if err == nil {
			return obj, nil
		}
		if errors.Is(err, gorm.ErrRecordNotFound) {
			_ = utils.InvalidateFileObjectHashCache(context.Background(), hash)
		} else {
			return nil, err
		}
	}
	var obj model.FileObject
	err := repo.Db.Where("hash = ?", hash).First(&obj).Error
	if err == nil {
		cacheFileObject(context.Background(), &obj)
	}
	return &obj, err
}

// GetFileObjectById finds a file object by ID.
// About Why This Not solve Be id -> cache Not Path -> id -> cache
func GetFileObjectById(id uint64) (*model.FileObject, error) {
	if cached, ok := utils.GetFileObjectFromCache(context.Background(), id); ok && cached != nil {
		cacheFileObject(context.Background(), cached)
		return cached, nil
	}

	var file model.FileObject
	err := repo.Db.Where("id = ?", id).First(&file).Error
	if err == nil {
		cacheFileObject(context.Background(), &file)
	}
	return &file, err
}

// IncreaseRefCount increments object reference count.
func IncreaseRefCount(id uint64) error {
	if _, err := adjustFileObjectRefCount(context.Background(), nil, id, 1); err != nil {
		return err
	}
	if err := repo.Db.Model(&model.FileObject{}).
		Where("id = ?", id).
		UpdateColumn("ref_count", gorm.Expr("ref_count + 1")).Error; err != nil {
		return err
	}
	_ = utils.InvalidateFileObjectCache(context.Background(), id)
	return nil
}

// DecreaseRefCount decrements object reference count.
// DecreaseRefCount atomically decrements ref_count under row lock.
func DecreaseRefCount(id uint64) (int, error) {
	remain, err := adjustFileObjectRefCount(context.Background(), nil, id, -1)
	if err != nil {
		return 0, err
	}
	_ = utils.InvalidateFileObjectCache(context.Background(), id)
	return int(remain), nil
}

// isFileObjectAvailable checks whether the physical object exists in storage.
func isFileObjectAvailable(ctx context.Context, obj *model.FileObject) (bool, error) {
	if obj == nil {
		return false, nil
	}
	if storage.Default == nil {
		return false, fmt.Errorf("storage not initialized")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	reader, _, err := storage.Default.GetObject(ctx, obj.BucketName, obj.ObjectName)
	if err != nil {
		return false, nil
	}
	_ = reader.Close()
	return true, nil
}

// CheckFileObjectAvailable checks whether a hash exists and is usable without creating user files.
// Returns nil object with a reason when upload is still required.
func CheckFileObjectAvailable(ctx context.Context, hash string, size int64) (*model.FileObject, string, error) {
	hash = strings.TrimSpace(hash)
	if hash == "" {
		return nil, "hash_missing", nil
	}
	obj, err := GetFileObjectByHash(hash)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, "hash_not_found", nil
		}
		return nil, "", err
	}
	if size > 0 && obj.Size > 0 && size != obj.Size {
		return nil, "size_mismatch", nil
	}
	available, err := isFileObjectAvailable(ctx, obj)
	if err != nil {
		return nil, "", err
	}
	if !available {
		return nil, "object_missing", nil
	}
	return obj, "", nil
}

// FastUpload handles hash-based instant upload.
func FastUpload(
	ctx context.Context,
	req *dto.UploadFileByHashRequest,
) (*dto.FastUploadResponse, error) {
	req.Hash = strings.TrimSpace(req.Hash)
	obj, err := GetFileObjectByHash(req.Hash)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return &dto.FastUploadResponse{
				Instant:    false,
				NeedUpload: true,
				Reason:     "hash_not_found",
				Hash:       req.Hash,
			}, nil
		}
		return nil, err
	}

	if req.Size > 0 && obj.Size > 0 && req.Size != obj.Size {
		return &dto.FastUploadResponse{
			Instant:    false,
			NeedUpload: true,
			Reason:     "size_mismatch",
			Hash:       req.Hash,
		}, nil
	}

	available, err := isFileObjectAvailable(ctx, obj)
	if err != nil {
		return nil, err
	}
	if !available {
		return &dto.FastUploadResponse{
			Instant:    false,
			NeedUpload: true,
			Reason:     "object_missing",
			Hash:       req.Hash,
		}, nil
	}

	var parentID *uint64
	if req.ParentId != 0 {
		parentID = &req.ParentId
	}
	userFile := &model.UserFile{
		UserID:   req.UserId,
		ParentID: parentID,
		Name:     req.FileName,
		ObjectID: &obj.ID,
		Size:     obj.Size,
		IsDir:    false,
	}
	if err := repo.Db.Transaction(func(tx *gorm.DB) error {
		if err := increaseRefCountTx(tx, obj.ID); err != nil {
			return err
		}
		return createUserFileEntryTx(tx, userFile)
	}); err != nil {
		return nil, err
	}
	_ = utils.InvalidateFileObjectCache(context.Background(), obj.ID)
	cacheFileObject(ctx, obj)
	finalizeUserFileEntry(userFile)
	return &dto.FastUploadResponse{
		Instant: true,
		FileId:  userFile.ID,
		Hash:    req.Hash,
	}, nil
}

// GetUploadSessionByHash loads an upload session by hash and user.
func GetUploadSessionByHash(userID uint64, hash string) (*model.UploadSession, error) {
	state, err := loadUploadSessionByHash(context.Background(), userID, hash)
	if err != nil {
		return nil, err
	}
	return &model.UploadSession{
		UploadID:    state.UploadID,
		UserID:      state.UserID,
		FileHash:    state.FileHash,
		FileName:    state.FileName,
		FileSize:    state.FileSize,
		ChunkSize:   state.ChunkSize,
		TotalChunks: state.TotalChunks,
	}, nil
}

// GetUploadSessionByUploadID loads an upload session by upload ID.
func GetUploadSessionByUploadID(uploadID string) (*model.UploadSession, error) {
	state, err := loadUploadSession(context.Background(), uploadID)
	if err != nil {
		return nil, err
	}
	return &model.UploadSession{
		UploadID:    state.UploadID,
		UserID:      state.UserID,
		FileHash:    state.FileHash,
		FileName:    state.FileName,
		FileSize:    state.FileSize,
		ChunkSize:   state.ChunkSize,
		TotalChunks: state.TotalChunks,
	}, nil
}

// GetUploadSessionByUploadID loads an upload session by upload ID.
//func GetUploadSessionByUploadID(uploadID string) (*model.UploadSession, error) {
//	var session model.UploadSession
//	if err := repo.Db.Where("upload_id = ?", uploadID).First(&session).Error; err != nil {
//		return nil, err
//	}
//	return &session, nil
//}

// CheckChunkNum loads uploaded chunks for a hash.
func CheckChunkNum(userID uint64, hash string, chunks *[]model.FileChunk) error {
	session, err := GetUploadSessionByHash(userID, hash)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}
		return err
	}
	uploaded, err := listUploadedChunkIndices(context.Background(), session.UploadID, session.TotalChunks)
	if err != nil {
		return err
	}
	if chunks == nil {
		return nil
	}
	out := make([]model.FileChunk, 0, len(uploaded))
	for _, idx := range uploaded {
		out = append(out, model.FileChunk{
			UploadID:   session.UploadID,
			ChunkIndex: idx,
			ChunkPath:  buildChunkObjectPath(session.UploadID, idx),
			Status:     1,
		})
	}
	*chunks = out
	return nil
}

// MultiPartFileInit initializes multipart upload.
func MultiPartFileInit(ctx context.Context, req dto.MultipartInitRequest) (*dto.MultiPartFileResponse, error) {
	req.Hash = strings.TrimSpace(req.Hash)
	if req.Hash == "" {
		req.Hash = "session_" + utils.GetToken()
		if err := CreateUploadSession(req); err != nil {
			return nil, err
		}
		uploadID, err := loadUploadID(req.UserId, req.Hash)
		if err != nil {
			return nil, err
		}
		return &dto.MultiPartFileResponse{
			Instant:  false,
			UploadID: uploadID,
			Uploaded: []int{},
		}, nil
	}
	if obj, err := GetFileObjectByHash(req.Hash); err == nil { // db
		available, checkErr := isFileObjectAvailable(ctx, obj) // minio
		if checkErr != nil {
			return nil, checkErr
		}
		if !available {
			goto uploadFlow
		}
		var parentID *uint64
		if req.ParentId != 0 {
			parentID = &req.ParentId
		}
		userFile := &model.UserFile{
			UserID:   req.UserId,
			ParentID: parentID,
			Name:     req.FileName,
			ObjectID: &obj.ID,
			Size:     obj.Size,
			IsDir:    false,
		}
		if err := repo.Db.Transaction(func(tx *gorm.DB) error {
			if err := increaseRefCountTx(tx, obj.ID); err != nil {
				return err
			}
			return createUserFileEntryTx(tx, userFile)
		}); err != nil {
			return nil, err
		}
		_ = utils.InvalidateFileObjectCache(context.Background(), obj.ID)
		cacheFileObject(ctx, obj)
		finalizeUserFileEntry(userFile)
		return &dto.MultiPartFileResponse{
			Instant: true,
		}, nil
	} else if !errors.Is(err, gorm.ErrRecordNotFound) { // 濡傛灉涓嶆槸娌℃湁鎵惧埌璁板綍 涔熷嵆鏄骇鐢熶簡鍏朵粬閿欒
		return nil, err
	}

uploadFlow: // hash noExist || no fileObject
	chunks := make([]model.FileChunk, 0)
	if err := CheckChunkNum(req.UserId, req.Hash, &chunks); err != nil {
		return nil, err
	}
	uploaded := make([]int, 0, len(chunks))
	for _, c := range chunks {
		uploaded = append(uploaded, c.ChunkIndex)
	}
	if len(uploaded) == 0 {
		if err := CreateUploadSession(req); err != nil {
			return nil, err
		}
	}
	uploadID, err := loadUploadID(req.UserId, req.Hash)
	if err != nil {
		return nil, err
	}
	return &dto.MultiPartFileResponse{
		Instant:  false,
		UploadID: uploadID,
		Uploaded: uploaded,
	}, nil
}

func loadUploadID(userID uint64, hash string) (string, error) {
	session, err := GetUploadSessionByHash(userID, hash)
	if err != nil {
		return "", err
	}
	return session.UploadID, nil
}

// CreateUploadSession creates an upload session record.
func CreateUploadSession(req dto.MultipartInitRequest) error {
	req.Hash = strings.TrimSpace(req.Hash)
	if req.Hash == "" {
		return errors.New("file_hash required")
	}
	state := &uploadSessionState{
		UploadID:    utils.GetToken(),
		UserID:      req.UserId,
		FileHash:    req.Hash,
		FileName:    req.FileName,
		FileSize:    req.Size,
		ChunkSize:   req.ChunkSize,
		TotalChunks: req.TotalChunks,
	}
	return saveUploadSession(context.Background(), state)
}

// UploadChunk stores a chunk in MinIO and the database.
// MinIO 涓庢暟鎹簱
func UploadChunk(
	ctx context.Context,
	req *dto.MultipartUploadChunkRequest,
) error {
	src, err := req.File.Open()
	if err != nil {
		return err
	}
	defer src.Close()
	objectPath := buildChunkObjectPath(req.UploadID, req.ChunkIndex)
	if storage.Default == nil {
		return fmt.Errorf("storage not initialized")
	}
	if err := storage.Default.PutObject(
		ctx,
		req.BucketName,
		objectPath,
		src,
		req.File.Size,
		storage.PutOptions{},
	); err != nil {
		return err
	}
	state, err := loadUploadSession(ctx, req.UploadID)
	if err != nil {
		if storage.Default != nil {
			_ = storage.Default.RemoveObject(ctx, req.BucketName, objectPath)
		}
		return err
	}
	reader, info, err := storage.Default.GetObject(ctx, req.BucketName, objectPath)
	if err != nil {
		if storage.Default != nil {
			_ = storage.Default.RemoveObject(ctx, req.BucketName, objectPath)
		}
		return err
	}
	_ = reader.Close()
	if err := markChunkUploaded(ctx, state, req.ChunkIndex, chunkMeta{
		Size: info.Size,
		ETag: strings.TrimSpace(info.ETag),
	}); err != nil {
		if storage.Default != nil {
			_ = storage.Default.RemoveObject(ctx, req.BucketName, objectPath)
		}
		return err
	}
	return nil
}

// FindAllChunkFile loads all chunks for completion.
func FindAllChunkFile(userID uint64, chunks *[]model.FileChunk, req dto.MultipartCompleteRequest) error {
	session, err := GetUploadSessionByHash(userID, req.FileHash)
	if err != nil {
		return err
	}
	return FindAllChunkFileByUploadID(session.UploadID, chunks)
}

// FindAllChunkFileByUploadID loads all chunks for completion by upload ID.
func FindAllChunkFileByUploadID(uploadID string, chunks *[]model.FileChunk) error {
	state, err := loadUploadSession(context.Background(), uploadID)
	if err != nil {
		return err
	}
	total := state.TotalChunks
	if total < 0 {
		total = 0
	}
	count, err := countUploadedChunks(context.Background(), uploadID)
	if err != nil {
		return err
	}
	if int(count) != total {
		return errors.New("chunks not complete")
	}
	if chunks == nil {
		return nil
	}
	out := make([]model.FileChunk, 0, total)
	for i := 0; i < total; i++ {
		out = append(out, model.FileChunk{
			UploadID:   uploadID,
			ChunkIndex: i,
			ChunkPath:  buildChunkObjectPath(uploadID, i),
			Status:     1,
		})
	}
	*chunks = out
	return nil
}

// FindMissingChunkIndices returns missing chunk indices for an upload.
func FindMissingChunkIndices(uploadID string, totalChunks int) ([]int, error) {
	uploadID = strings.TrimSpace(uploadID)
	if uploadID == "" {
		return nil, errors.New("upload_id required")
	}
	if totalChunks <= 0 {
		state, err := loadUploadSession(context.Background(), uploadID)
		if err != nil {
			return nil, err
		}
		totalChunks = state.TotalChunks
	}
	if totalChunks < 0 {
		return nil, errors.New("invalid total_chunks")
	}
	uploaded, err := listUploadedChunkIndices(context.Background(), uploadID, totalChunks)
	if err != nil {
		return nil, err
	}
	seen := make(map[int]struct{}, len(uploaded))
	for _, idx := range uploaded {
		seen[idx] = struct{}{}
	}
	missing := make([]int, 0)
	for i := 0; i < totalChunks; i++ {
		if _, ok := seen[i]; !ok {
			missing = append(missing, i)
		}
	}
	return missing, nil
}

type ChunkIntegrityResult struct {
	Missing []int
	Invalid []int
}

func dedupeSortedIndices(input []int) []int {
	if len(input) == 0 {
		return input
	}
	seen := make(map[int]struct{}, len(input))
	out := make([]int, 0, len(input))
	for _, idx := range input {
		if idx < 0 {
			continue
		}
		if _, ok := seen[idx]; ok {
			continue
		}
		seen[idx] = struct{}{}
		out = append(out, idx)
	}
	return out
}

// ValidateUploadChunks checks both bitmap completeness and chunk metadata/object integrity.
func ValidateUploadChunks(ctx context.Context, uploadID string, totalChunks int) (*ChunkIntegrityResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if storage.Default == nil {
		return nil, fmt.Errorf("storage not initialized")
	}
	uploadID = strings.TrimSpace(uploadID)
	if uploadID == "" {
		return nil, errors.New("upload_id required")
	}
	state, err := loadUploadSession(ctx, uploadID)
	if err != nil {
		return nil, err
	}
	if totalChunks <= 0 {
		totalChunks = state.TotalChunks
	}
	if totalChunks < 0 {
		return nil, errors.New("invalid total_chunks")
	}

	uploaded, err := listUploadedChunkIndices(ctx, uploadID, totalChunks)
	if err != nil {
		return nil, err
	}
	metaMap, err := loadChunkMetaMap(ctx, uploadID)
	if err != nil {
		return nil, err
	}
	uploadedSet := make(map[int]struct{}, len(uploaded))
	for _, idx := range uploaded {
		uploadedSet[idx] = struct{}{}
	}

	missing := make([]int, 0)
	invalid := make([]int, 0)
	for i := 0; i < totalChunks; i++ {
		if _, ok := uploadedSet[i]; !ok {
			missing = append(missing, i)
			continue
		}
		meta, ok := metaMap[i]
		if !ok {
			invalid = append(invalid, i)
			continue
		}
		chunkPath := buildChunkObjectPath(uploadID, i)
		reader, info, err := storage.Default.GetObject(ctx, config.AppConfig.BucketName, chunkPath)
		if err != nil {
			invalid = append(invalid, i)
			continue
		}
		_ = reader.Close()
		if meta.Size >= 0 && info.Size != meta.Size {
			invalid = append(invalid, i)
			continue
		}
		if strings.TrimSpace(meta.ETag) != "" && strings.TrimSpace(info.ETag) != "" &&
			!strings.EqualFold(strings.TrimSpace(meta.ETag), strings.TrimSpace(info.ETag)) {
			invalid = append(invalid, i)
			continue
		}
		if state.ChunkSize > 0 && totalChunks > 0 {
			expectedSize := state.ChunkSize
			if i == totalChunks-1 && state.FileSize > 0 {
				last := state.FileSize - int64(totalChunks-1)*state.ChunkSize
				if last > 0 {
					expectedSize = last
				}
			}
			if expectedSize > 0 && info.Size != expectedSize {
				invalid = append(invalid, i)
				continue
			}
		}
	}

	return &ChunkIntegrityResult{
		Missing: dedupeSortedIndices(missing),
		Invalid: dedupeSortedIndices(invalid),
	}, nil
}

// CompleteFile composes chunks and creates file records.
func CompleteFile(
	ctx context.Context,
	req dto.MultipartCompleteRequest,
	userName string,
) error {
	_, err := CompleteFileWithHash(ctx, req, userName)
	return err
}

// CompleteFileWithHash composes chunks, verifies hash (if provided), and creates records.
func CompleteFileWithHash(
	ctx context.Context,
	req dto.MultipartCompleteRequest,
	userName string,
) (string, error) {
	userId, err := FindIdByUsername(userName)
	if err != nil {
		return "", err
	}
	if ctx == nil {
		ctx = context.Background()
	}

	var session *model.UploadSession
	if strings.TrimSpace(req.UploadID) != "" {
		session, err = GetUploadSessionByUploadID(req.UploadID)
	} else if strings.TrimSpace(req.FileHash) != "" {
		session, err = GetUploadSessionByHash(userId, req.FileHash)
	} else {
		return "", errors.New("upload_id or file_hash required")
	}
	if err != nil {
		return "", err
	}
	if session.UserID != userId {
		return "", errors.New("upload session forbidden")
	}

	if req.TotalChunks <= 0 {
		req.TotalChunks = session.TotalChunks
	} else if req.TotalChunks != session.TotalChunks {
		return "", errors.New("total_chunks mismatch")
	}
	if req.FileSize <= 0 && session.FileSize > 0 {
		req.FileSize = session.FileSize
	}
	if req.FileName == "" {
		req.FileName = session.FileName
	}

	chunks := make([]model.FileChunk, 0)
	if err := FindAllChunkFileByUploadID(session.UploadID, &chunks); err != nil {
		return "", err
	}
	if len(chunks) != req.TotalChunks {
		return "", errors.New("chunks not complete")
	}
	integrity, err := ValidateUploadChunks(ctx, session.UploadID, req.TotalChunks)
	if err != nil {
		return "", err
	}
	if len(integrity.Missing) > 0 {
		return "", errors.New("chunks not complete")
	}
	if len(integrity.Invalid) > 0 {
		return "", errors.New("chunks invalid")
	}
	if storage.Default == nil {
		return "", fmt.Errorf("storage not initialized")
	}

	sessionState := &uploadSessionState{
		UploadID:    session.UploadID,
		UserID:      session.UserID,
		FileHash:    session.FileHash,
		TotalChunks: session.TotalChunks,
	}
	cleanupUploadData := func() {
		for _, c := range chunks {
			_ = storage.Default.RemoveObject(ctx, config.AppConfig.BucketName, c.ChunkPath)
		}
		_ = clearUploadState(ctx, sessionState)
	}
	writeObject := func(objectName string) error {
		if req.TotalChunks == 0 {
			if req.FileSize != 0 {
				return errors.New("invalid total_chunks")
			}
			return storage.Default.PutObject(
				ctx,
				config.AppConfig.BucketName,
				objectName,
				bytes.NewReader(nil),
				0,
				storage.PutOptions{},
			)
		}
		srcs := make([]storage.CopySource, 0, len(chunks))
		for _, c := range chunks {
			srcs = append(srcs, storage.CopySource{
				Bucket: config.AppConfig.BucketName,
				Object: c.ChunkPath,
			})
		}
		dst := storage.CopyDest{
			Bucket: config.AppConfig.BucketName,
			Object: objectName,
		}
		return storage.Default.ComposeObject(ctx, dst, srcs...)
	}

	dstObject := BuildTempObjectName(userName, session.UploadID)
	if strings.TrimSpace(req.FileHash) != "" {
		if existingObj, err := GetFileObjectByHash(req.FileHash); err == nil {
			available, checkErr := isFileObjectAvailable(ctx, existingObj)
			if checkErr == nil && !available && existingObj.ObjectName != "" {
				dstObject = existingObj.ObjectName
			}
		}
	}
	if err := writeObject(dstObject); err != nil {
		return "", err
	}
	hash, size, err := ComputeObjectHash(ctx, config.AppConfig.BucketName, dstObject)
	if err != nil {
		return "", err
	}
	if req.FileSize > 0 && size != req.FileSize {
		_ = storage.Default.RemoveObject(ctx, config.AppConfig.BucketName, dstObject)
		cleanupUploadData()
		return "", errors.New("file_size mismatch")
	}
	if strings.TrimSpace(req.FileHash) != "" && req.FileHash != hash {
		_ = storage.Default.RemoveObject(ctx, config.AppConfig.BucketName, dstObject)
		cleanupUploadData()
		return "", errors.New("file_hash mismatch")
	}

	var parentID *uint64
	if req.ParentId != 0 {
		parentID = &req.ParentId
	}
	if _, _, err := FinalizeUploadedObject(
		ctx,
		userId,
		parentID,
		req.FileName,
		size,
		config.AppConfig.BucketName,
		dstObject,
		hash,
	); err != nil {
		_ = storage.Default.RemoveObject(ctx, config.AppConfig.BucketName, dstObject)
		cleanupUploadData()
		return "", err
	}

	cleanupUploadData()
	return hash, nil
}

// FindObjectIdByName finds object ID by name.
func FindObjectIdByName(name string) (uint64, error) {
	var fileObject model.FileObject
	if err := repo.Db.Where("object_name = ?", name).First(&fileObject).Error; err != nil {
		return 0, err
	}
	return fileObject.ID, nil
}

// RemoveObject reduces ref count and schedules object deletion with delay.
func RemoveObject(objectId uint64) error {
	next, err := adjustFileObjectRefCount(context.Background(), nil, objectId, -1)
	if err != nil {
		return err
	}

	delay := config.AppConfig.FileObjectDeleteDelay
	now := time.Now()
	scheduledDelete := next <= 0
	updateFields := map[string]interface{}{
		"ref_count": next,
	}
	if scheduledDelete {
		updateFields["delete_status"] = model.FileObjectDeleteStatusPending
		if delay <= 0 {
			updateFields["delete_after"] = now
		} else {
			updateFields["delete_after"] = now.Add(delay)
		}
	} else {
		updateFields["delete_status"] = model.FileObjectDeleteStatusActive
		updateFields["delete_after"] = nil
	}
	if err := repo.Db.Model(&model.FileObject{}).
		Where("id = ?", objectId).
		Updates(updateFields).Error; err != nil {
		return err
	}
	_ = utils.InvalidateFileObjectCache(context.Background(), objectId)
	if !scheduledDelete {
		return nil
	}
	// Fire-and-forget cleanup attempt to reduce waiting for the next watchdog tick.
	go func() {
		_, _ = CleanupPendingFileObjects(context.Background(), 1)
	}()
	return nil
}
