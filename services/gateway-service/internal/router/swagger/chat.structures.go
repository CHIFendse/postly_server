package swagger

type CreateChatRequest struct {
	Username       string `json:"username" example:"ivan"`
	TargetUsername string `json:"target_username,omitempty" example:"alex"`
}

type CreateChatResponse struct {
	ChatId   string `json:"chat_id" example:"jnak323aAWEFc..."`
	TargetId string `json:"target_id" example:"32jfaASFjkndvs..."`
}

type CreateGroupRequest struct {
	Name        string   `json:"name" example:"Разработка Postly"`
	IsPrivate   bool     `json:"is_private" example:"true"`
	Members     []string `json:"members" example:"[\"alex\",\"dmitry\",\"elena\"]"`
	AvatarColor string   `json:"avatar_color" example:"#4A90E2"`
	AvatarFile  string   `json:"avatar_file,omitempty" example:"avatar_group_12.png"`
}

type CreateGroupResponse struct {
	GroupId  string   `json:"group_id" example:"ASD32e421xzc..."`
	Id       string   `json:"id" example:"ASD32e421xzc..."`
	NotFound []string `json:"not_found,omitempty" example:"[\"unknown_user\"]"`
}

type ClearChatRequest struct {
	ChatID string `json:"chat_id" example:"jnak323aAWEFc..."`
}

type DeleteChatRequest struct {
	ChatID string `json:"chat_id" example:"jnak323aAWEFc..."`
}

type DeleteGroupRequest struct {
	ChatID string `json:"chat_id" example:"ASD32e421xzc"`
}

type StatusResponse struct {
	Status bool `json:"status" example:"true"`
}