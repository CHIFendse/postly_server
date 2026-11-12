package swagger

type DeleteFriendRequest struct {
	FriendID string `json:"friend_id" example:"ey313afawrfq3.."`
}

type SendFriendRequestReq struct {
	Username       string `json:"username" example:"alex"`
	TargetUsername string `json:"target_username,omitempty" example:"alex"`
}

type SendFriendRequestResponse struct {
	RequestID string `json:"request_id" example:"ey124asfaaew32..."`
}

type FriendActionRequest struct {
	RequestID string `json:"request_id" example:"ey124asfaaew32..."`
}

type SuccessResponse struct {
	Success string `json:"success" example:"ok"`
}

type FriendItem struct {
	ID        string `json:"id" example:"ey313afawrfq3.."`
	Username  string `json:"username" example:"alex"`
	AvatarURL string `json:"avatar_url,omitempty" example:"/avatar?user_id=ey313afawrfq3.."`
}

type FriendRequestItem struct {
	RequestID string `json:"request_id" example:"ey124asfaaew32..."`
	SenderID  string `json:"sender_id" example:"ey313afawrfq3.."`
	Username  string `json:"username" example:"alex"`
	AvatarURL string `json:"avatar_url,omitempty" example:"/avatar?user_id=ey313afawrfq3.."`
	CreatedAt int64  `json:"created_at" example:"1700000000000"`
}

type SentRequestItem struct {
	RequestID  string `json:"request_id" example:"ey124asfaaew32..."`
	ReceiverID string `json:"receiver_id" example:"ey2131asafd2..."`
	Username   string `json:"username" example:"dmitry"`
	AvatarURL  string `json:"avatar_url,omitempty" example:"/avatar?user_id=ey2131asafd2..."`
	CreatedAt  int64  `json:"created_at" example:"1700000000000"`
}