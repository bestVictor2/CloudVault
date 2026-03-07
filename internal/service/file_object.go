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
	"gorm.io/gorm/clause"
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

// BuildObjectName builds object path for a user's hash.
func BuildObjectName(username, hash string) string {
	return fmt.Sprintf("files/%s/%s", username, hash)
} // minio 存储路径

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
		UserID:     userID,
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
// MinIO 删除对象
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
	if err := repo.Db.Model(&model.FileObject{}).Create(dir).Error; err != nil {
		return err
	}
	cacheFileObject(context.Background(), dir)
	return nil
}

func createFilesObjectTx(tx *gorm.DB, obj *model.FileObject) error {
	return tx.Model(&model.FileObject{}).Create(obj).Error
}

func increaseRefCountTx(tx *gorm.DB, id uint64) error {
	return tx.Model(&model.FileObject{}).
		Where("id = ?", id).
		UpdateColumn("ref_count", gorm.Expr("ref_count + 1")).Error
}

// GetFileByObject finds a file object by bucket and name.
func GetFileByObject(bucket, object string) (*model.FileObject, error) {
	if id, ok := utils.GetFileObjectIDByPath(context.Background(), bucket, object); ok {
		file, err := GetFileObjectById(id)
		if err == nil {
			return file, nil
		}
		if errors.Is(err, gorm.ErrRecordNotFound) { // 数据库中不存在但是缓存中存在 处理脏数据
			_ = utils.InvalidateFileObjectPathCache(context.Background(), bucket, object)
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
	if err := repo.Db.Model(&model.FileObject{}).
		Where("id = ?", id).
		UpdateColumn("ref_count", gorm.Expr("ref_count + 1")).Error; err != nil { // auto in db
		return err
	}
	_ = utils.InvalidateFileObjectCache(context.Background(), id)
	return nil
}

// DecreaseRefCount decrements object reference count.
func DecreaseRefCount(id uint64) (int, error) {
	var fileObject model.FileObject
	if err := repo.Db.Where("id = ?", id).First(&fileObject).Error; err != nil {
		return 0, err
	}
	if fileObject.RefCount > 1 {
		if err := repo.Db.Model(&model.FileObject{}).
			Where("id = ?", id).
			UpdateColumn("ref_count", gorm.Expr("ref_count - 1")).Error; err != nil {
			return 0, err
		}
		_ = utils.InvalidateFileObjectCache(context.Background(), id)
		return fileObject.RefCount - 1, nil
	}
	return 0, nil
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

// FastUpload handles hash-based instant upload.
func FastUpload(
	ctx context.Context,
	req *dto.UploadFileByHashRequest,
) (*dto.FastUploadResponse, error) {
	obj, err := GetFileObjectByHash(req.Hash)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return &dto.FastUploadResponse{
				Instant:    false,
				NeedUpload: true,
				Reason:     "hash_not_found",
			}, nil
		}
		return nil, err
	}

	if req.Size > 0 && obj.Size > 0 && req.Size != obj.Size {
		return &dto.FastUploadResponse{
			Instant:    false,
			NeedUpload: true,
			Reason:     "size_mismatch",
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
	}, nil
}

// GetUploadSessionByHash loads an upload session by hash and user.
func GetUploadSessionByHash(userID uint64, hash string) (*model.UploadSession, error) {
	var session model.UploadSession
	if err := repo.Db.
		Where("file_hash = ? AND user_id = ?", hash, userID).
		Order("id desc").
		First(&session).Error; err != nil {
		return nil, err
	}
	return &session, nil
}

// GetUploadSessionByUploadID loads an upload session by upload ID.
func GetUploadSessionByUploadID(uploadID string) (*model.UploadSession, error) {
	var session model.UploadSession
	if err := repo.Db.Where("upload_id = ?", uploadID).First(&session).Error; err != nil {
		return nil, err
	}
	return &session, nil
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
		if err == gorm.ErrRecordNotFound {
			return nil
		}
		return err
	}
	return repo.Db.
		Where("upload_id = ? AND status = 1", session.UploadID).
		Find(chunks).Error
}

// MultiPartFileInit initializes multipart upload.
func MultiPartFileInit(ctx context.Context, req dto.MultipartInitRequest) (*dto.MultiPartFileResponse, error) {
	req.Hash = strings.TrimSpace(req.Hash)
	if req.Hash == "" {
		req.Hash = "session_" + utils.GetToken()
		if err := CreateUploadSession(req); err != nil {
			return nil, err
		}
		var session model.UploadSession
		if err := repo.Db.Where("file_hash = ? AND user_id = ?", req.Hash, req.UserId).Order("id desc").First(&session).Error; err != nil {
			return nil, err
		}
		return &dto.MultiPartFileResponse{
			Instant:  false,
			UploadID: session.UploadID,
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
	} else if !errors.Is(err, gorm.ErrRecordNotFound) { // 如果不是没有找到记录 也即是产生了其他错误
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
	var uploadID string
	var session model.UploadSession
	if err := repo.Db.Where("file_hash = ? AND user_id = ?", req.Hash, req.UserId).Order("id desc").First(&session).Error; err == nil {
		uploadID = session.UploadID
	}
	return &dto.MultiPartFileResponse{
		Instant:  false,
		UploadID: uploadID,
		Uploaded: uploaded,
	}, nil
}

// CreateUploadSession creates an upload session record.
func CreateUploadSession(req dto.MultipartInitRequest) error {
	session := model.UploadSession{
		UploadID:    utils.GetToken(),
		UserID:      req.UserId,
		FileHash:    req.Hash,
		FileName:    req.FileName,
		FileSize:    req.Size,
		ChunkSize:   req.ChunkSize,
		TotalChunks: req.TotalChunks,
		Status:      0,
	}
	return repo.Db.Create(&session).Error
}

// UploadChunk stores a chunk in MinIO and the database.
// MinIO 与数据库
func UploadChunk(
	ctx context.Context,
	req *dto.MultipartUploadChunkRequest,
) error {
	src, err := req.File.Open()
	if err != nil {
		return err
	}
	defer src.Close()
	objectPath := fmt.Sprintf("chunks/%s/%d", req.UploadID, req.ChunkIndex)
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
	chunk := model.FileChunk{
		UploadID:   req.UploadID,
		ChunkIndex: req.ChunkIndex,
		ChunkSize:  req.File.Size,
		ChunkPath:  objectPath,
		Status:     1,
	}
	// 并发上传时 同一个分片被多次提交 导致数据库的混乱 所以需要幂等
	//
	return repo.Db.
		Clauses(clause.OnConflict{
			Columns: []clause.Column{
				{Name: "upload_id"},
				{Name: "chunk_index"},
			},
			DoUpdates: clause.AssignmentColumns([]string{
				"chunk_size",
				"chunk_path",
				"status",
				"updated_at",
			}),
		}).
		Create(&chunk).Error
}

// FindAllChunkFile loads all chunks for completion.
func FindAllChunkFile(userID uint64, chunks *[]model.FileChunk, req dto.MultipartCompleteRequest) error {
	var session model.UploadSession
	if err := repo.Db.Where("file_hash = ? AND user_id = ?", req.FileHash, userID).Order("id desc").First(&session).Error; err != nil {
		return err
	}
	return repo.Db.
		Where("upload_id = ? AND status = 1", session.UploadID).
		Order("chunk_index asc").
		Find(chunks).Error
}

// FindAllChunkFileByUploadID loads all chunks for completion by upload ID.
func FindAllChunkFileByUploadID(uploadID string, chunks *[]model.FileChunk) error {
	return repo.Db.
		Where("upload_id = ? AND status = 1", uploadID).
		Order("chunk_index asc").
		Find(chunks).Error
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
	if storage.Default == nil {
		return "", fmt.Errorf("storage not initialized")
	}

	cleanupUploadData := func() {
		for _, c := range chunks {
			_ = storage.Default.RemoveObject(ctx, config.AppConfig.BucketName, c.ChunkPath)
		}
		_ = repo.Db.Where("upload_id = ?", session.UploadID).Delete(&model.FileChunk{}).Error
		_ = repo.Db.Delete(&model.UploadSession{}, session.ID).Error
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

// RemoveObject reduces ref count and deletes object if needed.
func RemoveObject(objectId uint64) error {
	var fileObject model.FileObject
	if err := repo.Db.Where("id = ?", objectId).First(&fileObject).Error; err != nil {
		return err
	}
	remain, err := DecreaseRefCount(objectId)
	if err != nil {
		return err
	}
	if remain > 0 {
		return nil
	}
	// 如果是最后一个 则清理数据
	if err := DeleteMinioFile(&fileObject); err != nil {
		return err
	}
	if err := repo.Db.Delete(&model.FileObject{}, objectId).Error; err != nil {
		return err
	}
	_ = utils.InvalidateFileObjectCache(context.Background(), objectId)
	_ = utils.InvalidateFileObjectHashCache(context.Background(), fileObject.Hash)
	_ = utils.InvalidateFileObjectPathCache(context.Background(), fileObject.BucketName, fileObject.ObjectName)
	var session model.UploadSession
	if err := repo.Db.Where("file_hash = ? AND user_id = ?", fileObject.Hash, fileObject.UserID).Order("id desc").First(&session).Error; err != nil {
		return nil
	}
	return repo.Db.Where("upload_id = ?", session.UploadID).Delete(&model.FileChunk{}).Error
}
