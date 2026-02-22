package task

import (
	"CloudVault/config"
	"CloudVault/internal/activity"
	"CloudVault/internal/mq"
	"CloudVault/internal/repo"
	"CloudVault/internal/service"
	"CloudVault/model"
	"CloudVault/utils"
	"context"
	"encoding/json"
	"time"
)

// DownloadMessage is the payload sent to the worker.
type DownloadMessage struct {
	TaskID  uint64 `json:"task_id"`
	Attempt int    `json:"attempt"`
}

// CreateDownloadTask creates and enqueues a download task.
func CreateDownloadTask(userID uint64, url, fileName string) (*model.DownloadTask, error) {
	token := utils.GetToken()
	task := &model.DownloadTask{
		UserID:     userID,
		Type:       "http",
		Source:     url,
		Bucket:     config.AppConfig.BucketName,
		ObjectName: token,
		FileName:   fileName,
		Status:     "pending",
		Progress:   0,
	}
	if err := repo.Db.Create(task).Error; err != nil {
		return nil, err
	}
	msg := DownloadMessage{
		TaskID:  task.ID,
		Attempt: 0,
	}
	body, err := json.Marshal(msg)
	if err != nil {
		markDownloadTaskFailed(task.ID, err)
		return nil, err
	}
	publisher, err := mq.GetPublisher()
	if err != nil {
		markDownloadTaskFailed(task.ID, err)
		return nil, err
	}
	if err := publisher.PublishTask(context.Background(), body); err != nil {
		markDownloadTaskFailed(task.ID, err)
		return nil, err
	}
	return task, nil
}

// ListDownloadTasks lists download tasks for a user.
func ListDownloadTasks(userID uint64, limit int) ([]model.DownloadTask, error) {
	if limit <= 0 {
		limit = 20
	}
	var tasks []model.DownloadTask
	err := repo.Db.Where("user_id = ?", userID).
		Order("created_at DESC").
		Limit(limit).
		Find(&tasks).Error
	return tasks, err
}

// ProcessDownloadTask executes a download task.
func ProcessDownloadTask(ctx context.Context, taskID uint64) error {
	var task model.DownloadTask
	if err := repo.Db.Where("id = ?", taskID).First(&task).Error; err != nil {
		return err
	}
	if task.Status == "completed" {
		return nil
	}
	startedAt := time.Now()
	res := repo.Db.Model(&model.DownloadTask{}).
		Where("id = ? AND status IN ?", taskID, []string{"pending", "retrying"}).
		Updates(map[string]interface{}{
			"status":     "running",
			"progress":   0,
			"started_at": &startedAt,
			"error_msg":  "",
		})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return nil
	}

	size, err := service.DownloadByHTTP(
		ctx,
		task.Source,
		task.ObjectName,
		task.UserID,
	)
	if err != nil {
		return err
	}

	userName, err := service.FindUserNameById(task.UserID)
	if err != nil {
		return err
	}

	objectName := service.BuildObjectName(userName, task.ObjectName)
	fileObj := &model.FileObject{
		UserID:     task.UserID,
		Hash:       task.ObjectName,
		BucketName: task.Bucket,
		ObjectName: objectName,
		Size:       size,
		RefCount:   1,
	}
	if err := service.CreateFilesObject(fileObj); err != nil {
		return err
	}

	userFile := &model.UserFile{
		UserID:   task.UserID,
		Name:     task.FileName,
		IsDir:    false,
		ObjectID: &fileObj.ID,
		Size:     size,
	}
	if err := service.CreateUserFileEntry(userFile); err != nil {
		return err
	}

	finishedAt := time.Now()
	if err := repo.Db.Model(&task).Updates(map[string]interface{}{
		"status":      "completed",
		"progress":    100,
		"finished_at": &finishedAt,
	}).Error; err != nil {
		return err
	}
	_ = activity.Emit(context.Background(), task.UserID, activity.ActionDownload, userFile.ID, size)
	return nil
}

func markDownloadTaskFailed(taskID uint64, err error) {
	finishedAt := time.Now()
	_ = repo.Db.Model(&model.DownloadTask{}).
		Where("id = ?", taskID).
		Updates(map[string]interface{}{
			"status":      "failed",
			"error_msg":   err.Error(),
			"finished_at": &finishedAt,
		}).Error
}
