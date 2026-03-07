package utils

import "github.com/gin-gonic/gin"

// Success writes a success JSON response.
func Success(c *gin.Context, data interface{}) {
	c.JSON(200, gin.H{
		"code": 0,
		"msg":  "ok",
		"data": data,
	})
}

// Fail writes an error JSON response.
func Fail(c *gin.Context, err error) {
	c.JSON(400, gin.H{
		"code": -1,
		"msg":  err.Error(),
	})
}
