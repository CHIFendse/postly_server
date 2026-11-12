package swagger

type LoginRequest struct {
	Username string `json:"username" example:"ivan"`
	Password string `json:"password" example:"secret123"`
}

type LoginResponse struct {
	Token    string `json:"token" example:"eyJhbGciOi..."`
	ID       string `json:"id" example:"usr_12345"`
	Username string `json:"username" example:"ivan"`
}

type VerifyResponse struct {
	UserID string `json:"user_id" example:"usr_12345"`
}

type AuthErrorResponse struct {
	Message string `json:"message" example:"Неверный логин или пароль"`
}