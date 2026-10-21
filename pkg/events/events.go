package events

import (
	"time"

	"github.com/google/uuid"
)

type Event struct {
	ID            string            `json:"id"`
	Version       int               `json:"version"`
	Type          string            `json:"type"`
	Time          time.Time         `json:"time"`
	ActorID       string            `json:"actor_id"`
	RecipientsIDs []string          `json:"recipients_ids"`
	Entity        map[string]string `json:"entity"`
	Data          map[string]string `json:"data,omitempty"`
}

const FriendRequestCreated = "friend.request.created"
const FriendRequestAccepted = "friend.request.accepted"
const MessageCreated = "message.created"

func NewEvent(
	typeEvent, actorID string,
	recipientsIDs []string,
	entity, data map[string]string) *Event {
	id := uuid.New().String()
	e := &Event{ID: id, Version: 1, Time: time.Now().UTC(),
		Type: typeEvent, ActorID: actorID, RecipientsIDs: recipientsIDs,
		Data: data, Entity: entity, }
	return e
}
