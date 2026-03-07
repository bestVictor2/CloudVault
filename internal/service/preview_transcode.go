package service

import (
	"CloudVault/config"
	"CloudVault/internal/storage"
	"CloudVault/model"
	"CloudVault/utils"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

var previewTranscodeLockMap sync.Map

func configPreviewTranscodeEnabled() bool {
	return config.AppConfig.PreviewTranscodeEnabled &&
		strings.TrimSpace(config.AppConfig.PreviewTranscodeFFmpeg) != ""
}

func shouldTranscodeForPreview(fileName string) bool {
	switch strings.ToLower(path.Ext(fileName)) {
	case ".avi", ".mkv", ".mov", ".wmv", ".flv", ".mpeg", ".mpg", ".ts", ".m4v", ".3gp", ".rmvb", ".vob":
		return true
	default:
		return false
	}
}

func getOrBuildTranscodedPreviewURL(
	ctx context.Context,
	file *model.UserFile,
	obj *model.FileObject,
	expiry time.Duration,
) (string, error) {
	if storage.Default == nil {
		return "", fmt.Errorf("storage not initialized")
	}
	outputObject := buildTranscodedPreviewObjectName(obj)
	outputName := buildPreviewMP4Name(file.Name)

	if objectExists(ctx, obj.BucketName, outputObject) {
		return buildInlinePreviewURL(ctx, obj.BucketName, outputObject, outputName, expiry)
	}

	lock := getPreviewTranscodeLock(obj.ID)
	lock.Lock()
	defer lock.Unlock()

	if objectExists(ctx, obj.BucketName, outputObject) {
		return buildInlinePreviewURL(ctx, obj.BucketName, outputObject, outputName, expiry)
	}
	if err := transcodeAndUploadPreview(ctx, obj.BucketName, obj.ObjectName, obj.BucketName, outputObject, file.Name); err != nil {
		return "", err
	}
	return buildInlinePreviewURL(ctx, obj.BucketName, outputObject, outputName, expiry)
}

func buildTranscodedPreviewObjectName(obj *model.FileObject) string {
	hash := strings.TrimSpace(obj.Hash)
	if hash == "" {
		hash = "nohash"
	}
	return fmt.Sprintf("preview/transcoded/%d_%s.mp4", obj.ID, hash)
}

func buildPreviewMP4Name(original string) string {
	base := strings.TrimSpace(strings.TrimSuffix(original, path.Ext(original)))
	if base == "" {
		base = "preview"
	}
	return base + ".mp4"
}

func buildInlineDisposition(fileName string) string {
	safe := utils.SanitizeHeaderFilename(fileName)
	return fmt.Sprintf("inline; filename=\"%s\"", safe)
}

func getPreviewTranscodeLock(objectID uint64) *sync.Mutex {
	lock, _ := previewTranscodeLockMap.LoadOrStore(objectID, &sync.Mutex{})
	return lock.(*sync.Mutex)
}

func objectExists(ctx context.Context, bucket, object string) bool {
	if storage.Default == nil {
		return false
	}
	reader, _, err := storage.Default.GetObject(ctxOrBackground(ctx), bucket, object)
	if err != nil {
		return false
	}
	_ = reader.Close()
	return true
}

func transcodeAndUploadPreview(
	ctx context.Context,
	srcBucket string,
	srcObject string,
	dstBucket string,
	dstObject string,
	originName string,
) error {
	reader, info, err := storage.Default.GetObject(ctxOrBackground(ctx), srcBucket, srcObject)
	if err != nil {
		return err
	}
	defer reader.Close()

	if maxBytes := config.AppConfig.PreviewTranscodeMaxBytes; maxBytes > 0 && info.Size > maxBytes {
		return fmt.Errorf("preview transcode skipped: source too large")
	}

	tmpDir, err := os.MkdirTemp("", "cloudvault-preview-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmpDir)

	inputPath := filepath.Join(tmpDir, "input"+strings.ToLower(path.Ext(originName)))
	outputPath := filepath.Join(tmpDir, "output.mp4")

	inputFile, err := os.Create(inputPath)
	if err != nil {
		return err
	}
	if _, err := io.Copy(inputFile, reader); err != nil {
		_ = inputFile.Close()
		return err
	}
	if err := inputFile.Close(); err != nil {
		return err
	}

	ffmpeg := strings.TrimSpace(config.AppConfig.PreviewTranscodeFFmpeg)
	if ffmpeg == "" {
		ffmpeg = "ffmpeg"
	}
	timeout := config.AppConfig.PreviewTranscodeTimeout
	if timeout <= 0 {
		timeout = 5 * time.Minute
	}
	cmdCtx, cancel := context.WithTimeout(ctxOrBackground(ctx), timeout)
	defer cancel()

	cmd := exec.CommandContext(
		cmdCtx,
		ffmpeg,
		"-y",
		"-i", inputPath,
		"-movflags", "+faststart",
		"-c:v", "libx264",
		"-preset", "veryfast",
		"-pix_fmt", "yuv420p",
		"-c:a", "aac",
		"-b:a", "128k",
		outputPath,
	)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("preview transcode failed: %w (%s)", err, compactErrorText(output))
	}

	stat, err := os.Stat(outputPath)
	if err != nil {
		return err
	}
	if stat.Size() <= 0 {
		return fmt.Errorf("preview transcode output empty")
	}

	out, err := os.Open(outputPath)
	if err != nil {
		return err
	}
	defer out.Close()

	return storage.Default.PutObject(
		ctxOrBackground(ctx),
		dstBucket,
		dstObject,
		out,
		stat.Size(),
		storage.PutOptions{ContentType: "video/mp4"},
	)
}
