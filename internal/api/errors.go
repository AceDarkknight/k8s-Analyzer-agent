package api

const (
	CodeOK            = 0
	CodeBadRequest    = 40000
	CodeNotFound      = 40400
	CodeInternalError = 50000
	CodeStoreError    = 50001
)

var codeMessages = map[int]string{
	CodeOK:            "ok",
	CodeBadRequest:    "invalid request parameters",
	CodeNotFound:      "resource not found",
	CodeInternalError: "internal server error",
	CodeStoreError:    "trace store error",
}
