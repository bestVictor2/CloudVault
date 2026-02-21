package dto

import "mime/multipart"

type UploadFileByHashRequest struct {
	UserId   uint64                `json:"-"`
	FileId   uint64                `json:"file_id"`
	FileName string                `json:"file_name" binding:"required"`
	Size     int64                 `json:"size" binding:"required"`
	Hash     string                `json:"hash" binding:"required"`
	ParentId uint64                `json:"parent_id"`
	File     *multipart.FileHeader `json:"-"`
	IsDir    bool                  `json:"is_dir"`
}

type MultipartInitRequest struct {
	UserId      uint64 `json:"user_id"`
	FileId      uint64 `json:"file_id"`
	FileName    string `json:"file_name"`
	Size        int64  `json:"size"`
	Hash        string `json:"hash"`
	ChunkSize   int64  `json:"chunk_size"`
	TotalChunks int    `json:"total_chunks"`
	ParentId    uint64 `json:"parent_id"`
}

type MultipartUploadChunkRequest struct {
	UploadID   string
	BucketName string
	ChunkIndex int
	File       *multipart.FileHeader
}

type MultipartCompleteRequest struct {
	FileId      uint64 `json:"file_id"`
	FileHash    string `json:"file_hash" binding:"required"`
	FileName    string `json:"file_name" binding:"required"`
	FileSize    int64  `json:"file_size" binding:"gte=0"`
	TotalChunks int    `json:"total_chunks" binding:"gte=0"`
	ParentId    uint64 `json:"parent_id"`
	IsDir       bool   `json:"is_dir"`
}

type HttpOfflineDownloadRequest struct {
	URL      string `json:"url" binding:"required"`
	FileName string `json:"file_name" binding:"required"`
}

type URLUploadRequest struct {
	URL      string `json:"url" binding:"required"`
	FileName string `json:"file_name"`
	ParentID uint64 `json:"parent_id"`
}

type MinioDownloadRequest struct {
	FileID   uint64 `json:"file_id"`
	FileHash string `json:"file_hash"`
}

type ArchiveDownloadRequest struct {
	FileIDs []uint64 `json:"file_ids" binding:"required"`
	Name    string   `json:"name"`
}

type LoginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type RegisterRequest struct {
	Username      string `json:"username" binding:"required"`
	FirstPassword string `json:"first-password" binding:"required"`
	LastPassword  string `json:"second-password" binding:"required"`
	Email         string `json:"email" binding:"required"`
}

type CreateShareRequest struct {
	FileID     uint64 `json:"file_id"`
	ExpireDays int    `json:"expire_days"`
	NeedCode   bool   `json:"need_code"`
}

type FileListRequest struct {
	ParentID  *uint64 `json:"parent_id"`
	Page      int     `json:"page"`
	PageSize  int     `json:"page_size"`
	OrderBy   string  `json:"order_by"`
	OrderDesc bool    `json:"order_desc"`
}

type FileSearchRequest struct {
	Query     string  `json:"query" binding:"required"`
	ParentID  *uint64 `json:"parent_id"`
	Page      int     `json:"page"`
	PageSize  int     `json:"page_size"`
	OrderBy   string  `json:"order_by"`
	OrderDesc bool    `json:"order_desc"`
}

type FileRenameRequest struct {
	FileID  uint64 `json:"file_id" binding:"required"`
	NewName string `json:"new_name" binding:"required"`
}

type FileMoveRequest struct {
	FileIDs  []uint64 `json:"file_ids" binding:"required"`
	TargetID *uint64  `json:"target_id"`
}

type FileCopyRequest struct {
	FileIDs  []uint64 `json:"file_ids" binding:"required"`
	TargetID *uint64  `json:"target_id"`
}

type FolderUploadRequest struct {
	ParentID uint64 `json:"parent_id"`
	Path     string `json:"path"`
	Name     string `json:"name"`
}

type BatchDeleteRequest struct {
	FileIDs []uint64 `json:"file_ids" binding:"required"`
}

type RestoreFileRequest struct {
	FileID uint64 `json:"file_id" binding:"required"`
}

type DeleteFileRequest struct {
	FileID uint64 `json:"file_id" binding:"required"`
}

type UpdateUserProfileRequest struct {
	NickName  *string `json:"nick_name"`
	Email     *string `json:"email"`
	AvatarURL *string `json:"avatar_url"`
	Bio       *string `json:"bio"`
}

type FavoriteRequest struct {
	FileID uint64 `json:"file_id" binding:"required"`
}
