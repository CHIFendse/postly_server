package swagger

type RegisterRequest struct {
	Username string `json:"username" example:"ivan"`
	Password string `json:"password" example:"secret123"`
	Email    string `json:"email" example:"ivan@example.com"`
	Phone    string `json:"phone,omitempty" example:"+79991234567"`
}

type GetAvatarUploadURLRequest struct {
	FileName    string `json:"file_name" example:"avatar.png"`
	ContentType string `json:"content_type" example:"image/png" enums:"image/jpeg,image/png,image/webp"`
}

type GetAvatarUploadURLResponse struct {
	UploadURL string `json:"upload_url" example:"https://s3.storage.local/upload/..."`
	S3Key     string `json:"s3_key" example:"avatars/usr_12345/avatar.png"`
}

type SetAvatarRequest struct {
	S3Key string `json:"s3_key" example:"avatars/usr_12345/avatar.png"`
}

type DeleteAvatarRequest struct {
	S3Key string `json:"s3_key" example:"avatars/usr_12345/avatar.png"`
}

type AvatarItem struct {
	URL       string `json:"url" example:"/avatar?key=avatars%2Fusr_12345%2Favatar.png"`
	S3Key     string `json:"s3_key" example:"avatars/usr_12345/avatar.png"`
	CreatedAt int64  `json:"created_at" example:"1700000000000"`
}