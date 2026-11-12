package swagger

type WSIncomingMessage struct {
	Type           string `json:"type" example:"text" enums:"text,image,voice,file,video,TYPING,CALL_OFFER,CALL_ANSWER"`
	ChatID         string `json:"chat_id" example:"cht_102938"`
	Username       string `json:"username,omitempty" example:"bogdan"`
	Text           string `json:"text,omitempty" example:"Привет, как дела?"`
	FileURL        string `json:"file_url,omitempty" example:"uploads/usr_1/image.png"`
	FileName       string `json:"file_name,omitempty" example:"photo.png"`
	FileSize       string `json:"file_size,omitempty" example:"1048576"`
}

type WSOutgoingMessage struct {
	Type        string `json:"type" example:"NEW_MESSAGE" enums:"NEW_MESSAGE,TYPING,CLEAR_CHAT,DELETE_CHAT,NEW_CHAT"`
	ID          string `json:"id,omitempty" example:"msg_998877"`
	ChatID      string `json:"chat_id" example:"cht_102938"`
	SenderID    string `json:"sender_id,omitempty" example:"usr_123"`
	Username    string `json:"username,omitempty" example:"bogdan"`
	Text        string `json:"text,omitempty" example:"Привет, как дела?"`
	MessageType string `json:"message_type,omitempty" example:"text"`
	FileURL     string `json:"file_url,omitempty" example:"/file?message_id=msg_998877"`
	FileName    string `json:"file_name,omitempty" example:"photo.png"`
	FileSize    string `json:"file_size,omitempty" example:"1048576"`
	CreatedAt   string `json:"created_at,omitempty" example:"2026-11-12T18:14:00Z"`
}