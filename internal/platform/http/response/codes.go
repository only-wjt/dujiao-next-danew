package response

const (
	CodeOK              = 0
	CodeBadRequest      = 400
	CodeUnauthorized    = 401
	CodeForbidden       = 403
	CodeNotFound        = 404
	CodeCaptchaRequired = 428 // 需要先完成人机验证
	CodeTooManyRequests = 429
	CodeInternal        = 500
)
