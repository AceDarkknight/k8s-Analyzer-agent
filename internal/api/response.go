package api

import (
	"github.com/gin-gonic/gin"
)

type responseBody struct {
	Code    int         `json:"code"`
	Message string      `json:"message"`
	Data    interface{} `json:"data"`
}

func writeJSON(c *gin.Context, httpStatus int, code int, data interface{}) {
	c.JSON(httpStatus, responseBody{
		Code:    code,
		Message: codeMessages[code],
		Data:    data,
	})
}

func writeError(c *gin.Context, httpStatus int, code int) {
	writeJSON(c, httpStatus, code, nil)
}
