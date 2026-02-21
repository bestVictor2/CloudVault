package router

import (
	"Go_Pan/internal/handler"
	"Go_Pan/utils"

	"github.com/gin-gonic/gin"
)

// InitRouter builds API routes.
func InitRouter() *gin.Engine {
	r := gin.Default()
	r.Use(utils.CORSMiddleware())

	api := r.Group("/api")
	{
		api.POST("/register", handler.Register)
		api.GET("/activate", handler.Activate)
		api.POST("/login", handler.Login)

		auth := api.Group("")
		auth.Use(utils.AuthMiddleware())

		file := auth.Group("/file")
		{
			file.POST("/list", handler.GetFileList)
			file.POST("/search", handler.SearchFiles)
			file.POST("/rename", handler.RenameFile)
			file.POST("/move", handler.MoveFiles)
			file.POST("/copy", handler.CopyFiles)
			file.POST("/folder", handler.CreateFolder)
			file.POST("/delete", handler.BatchDeleteFiles)
			file.POST("/upload/hash", handler.UploadFileByHash)
			file.POST("/upload/url", handler.UploadFileByURL)
			file.POST("/download/minio", handler.MinioDownloadFile)
			file.POST("/download/url", handler.MinioDownloadURL)
			file.POST("/upload/multipart/init", handler.MultiPartFileInit)
			file.POST("/upload/multipart/chunk", handler.MultipartUploadChunk)
			file.POST("/upload/multipart/complete", handler.MultipartComplete)
			file.POST("/download/offline", handler.HttpOfflineDownload)
			file.POST("/download/archive", handler.DownloadArchive)
			file.GET("/download/tasks", handler.ListDownloadTasks)
			file.GET("/preview/:fileID", handler.PreviewFile)
		}

		recycle := auth.Group("/recycle")
		{
			recycle.POST("/list", handler.ListRecycleFiles)
			recycle.POST("/restore", handler.RestoreFile)
			recycle.POST("/delete", handler.DeleteFileRecord)
		}

		share := auth.Group("/share")
		{
			share.POST("/create", handler.CreateShareHandler)
			share.GET("/access/logs", handler.GetShareAccessLogs)
			share.GET("/access/stats", handler.GetShareAccessStats)
		}
		user := auth.Group("/user")
		{
			user.GET("/me", handler.GetCurrentUser)
			user.PUT("/me", handler.UpdateCurrentUser)
			user.GET("/favorites", handler.ListUserFavorites)
			user.POST("/favorites", handler.AddUserFavorite)
			user.DELETE("/favorites/:fileID", handler.RemoveUserFavorite)
			user.GET("/recent", handler.ListUserRecent)
			user.GET("/common-dirs", handler.ListUserCommonDirs)
			user.GET("/activity/summary", handler.GetUserActivitySummary)
		}
		api.GET("/share/download/:shareID", handler.ShareDownload)
	}
	return r
}
