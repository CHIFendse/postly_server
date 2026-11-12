package swagger

type GetUploadURLRequest struct {
	FileName    string `json:"file_name" example:"image.png"`
	ContentType string `json:"content_type" example:"image/png"`
	FileSize    int64  `json:"file_size" example:"1048576"`
	MessageType string `json:"message_type" example:"image" enums:"image,video,voice,file"`
}

type GetUploadURLResponse struct {
	UploadURL string `json:"upload_url" example:"https://s3.storage.local/upload/..."`
	S3Key     string `json:"s3_key" example:"images/ey313afawrfq3../image.png"`
}

type SendMessageRequest struct {
	ChatID string `json:"chat_id" example:"eyew3rsdzdszs..."`
	Text   string `json:"text" example:"Hello, how are you?"`
}

type SendMessageResponse struct {
	ID string `json:"id" example:"ey123asdfsfs..."`
}

type DeleteMessageRequest struct {
	MessageID string `json:"message_id" example:"ey123asdfsfs..."`
}

type DeleteMessageResponse struct {
	ChatID string `json:"chat_id" example:"eyew3rsdzdszs..."`
}

type GetMoreMessagesRequest struct {
	ChatID               string `json:"chat_id" example:"eyew3rsdzdszs..."`
	LastMessageID        string `json:"last_message_id" example:"ey123asdfsfs..."`
	LastMessageCreatedAt int64  `json:"last_message_created_at" example:"1700000000"`
}

type MessageItem struct {
	ID        string `json:"id" example:"ey123asdfsfs..."`
	ChatID    string `json:"chat_id" example:"eyew3rsdzdszs..."`
	SenderID  string `json:"sender_id" example:"ey313afawrfq3.."`
	Text      string `json:"text" example:"Hello world"`
	CreatedAt int64  `json:"created_at" example:"1700000000000"`
	Username  string `json:"username" example:"alex"`
	Type      string `json:"message_type" example:"text" enums:"text,image,video,voice,file"`
	FileURL   string `json:"file_url,omitempty" example:"/file?message_id=ey123asdfsfs..."`
	FileName  string `json:"file_name,omitempty" example:"document.pdf"`
	FileSize  string `json:"file_size,omitempty" example:"204800"`
}