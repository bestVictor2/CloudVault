package dto

// FastUploadResponse is the response for instant upload.
type FastUploadResponse struct {
	Instant    bool   `json:"instant"`
	NeedUpload bool   `json:"need_upload,omitempty"`
	Reason     string `json:"reason,omitempty"`
	FileId     uint64 `json:"file_id,omitempty"`
	UploadId   string `json:"upload_id,omitempty"`
	Hash       string `json:"hash,omitempty"`
	Reused     bool   `json:"reused,omitempty"`
}

// MultiPartFileResponse is the response for multipart uploads.
type MultiPartFileResponse struct {
	Instant  bool   `json:"instant"`
	UploadID string `json:"upload_id,omitempty"`
	Uploaded []int  `json:"uploaded,omitempty"`
}
