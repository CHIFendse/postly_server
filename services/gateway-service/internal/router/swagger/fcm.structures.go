package swagger

type FCMRegisterRequest struct {
	FCMToken string `json:"fcm_token" example:"dGhpcy1pcy1hLWZha2UtZmNtLXRva2Vu..."`
}

type OKResponse struct {
	OK bool `json:"ok" example:"true"`
}