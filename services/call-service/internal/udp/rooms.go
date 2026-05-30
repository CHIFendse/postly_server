package udp

import (
	"log"
	"net"
	"sync"
	"time"
)

type UserAddr struct {
	mu           sync.Mutex
	Conn         net.Conn
	LastSeen     time.Time
	LastSeq      uint32
	LostPackets  int
	TotalPackets int
}

type RoomManager struct {
	Mu         sync.RWMutex
	Rooms      map[string]map[string]*UserAddr
	AddrToUser map[string]string
	UserToRoom map[string]string
}

func NewRoomManager() *RoomManager {
	return &RoomManager{
		Rooms:      make(map[string]map[string]*UserAddr),
		AddrToUser: make(map[string]string),
		UserToRoom: make(map[string]string),
	}
}

func (rm *RoomManager) AddUser(roomID, userID string, conn net.Conn) bool {
	rm.Mu.Lock()
	defer rm.Mu.Unlock()

	isNew := false
	addrStr := conn.RemoteAddr().String()

	if oldRoomID, exists := rm.UserToRoom[userID]; exists {
		if oldUser, ok := rm.Rooms[oldRoomID][userID]; ok {
			if oldUser.Conn.RemoteAddr().String() != addrStr {
				delete(rm.AddrToUser, oldUser.Conn.RemoteAddr().String())
			}
		}
	} else {
		isNew = true
	}

	if _, ok := rm.Rooms[roomID]; !ok {
		rm.Rooms[roomID] = make(map[string]*UserAddr)
	}
	rm.Rooms[roomID][userID] = &UserAddr{Conn: conn, LastSeen: time.Now()}
	rm.AddrToUser[addrStr] = userID
	rm.UserToRoom[userID] = roomID
	return isNew
}

func (rm *RoomManager) GetParticipants(addrStr string) ([]net.Conn, bool) {
	rm.Mu.RLock()
	defer rm.Mu.RUnlock()

	userID, ok := rm.AddrToUser[addrStr]
	if !ok {
		return nil, false
	}
	roomID, ok := rm.UserToRoom[userID]
	if !ok {
		return nil, false
	}
	conns := make([]net.Conn, 0)
	for _, u := range rm.Rooms[roomID] {
		conns = append(conns, u.Conn)
	}
	return conns, true
}

func (rm *RoomManager) UpdateActivity(addrStr string) {
	rm.Mu.RLock()
	userID, ok := rm.AddrToUser[addrStr]
	if !ok {
		rm.Mu.RUnlock()
		return
	}
	roomID := rm.UserToRoom[userID]
	user := rm.Rooms[roomID][userID]
	rm.Mu.RUnlock()

	user.mu.Lock()
	user.LastSeen = time.Now()
	user.mu.Unlock()
}

func (rm *RoomManager) RemoveUserByAddr(addrStr string) {
	rm.Mu.Lock()
	defer rm.Mu.Unlock()

	userID, ok := rm.AddrToUser[addrStr]
	if !ok {
		return
	}
	roomID := rm.UserToRoom[userID]
	log.Printf("[DTLS] user %s left", userID)
	delete(rm.AddrToUser, addrStr)
	delete(rm.UserToRoom, userID)
	if room, ok := rm.Rooms[roomID]; ok {
		delete(room, userID)
	}
}

func (rm *RoomManager) AnalyzePacketLoss(addrStr string, seq uint32) string {
	rm.Mu.RLock()
	userID, ok := rm.AddrToUser[addrStr]
	if !ok {
		rm.Mu.RUnlock()
		return ""
	}
	roomID := rm.UserToRoom[userID]
	user := rm.Rooms[roomID][userID]
	rm.Mu.RUnlock()

	user.mu.Lock()
	defer user.mu.Unlock()

	user.TotalPackets++
	if user.LastSeq != 0 && seq > user.LastSeq+1 {
		user.LostPackets += int(seq - user.LastSeq - 1)
	}
	user.LastSeq = seq

	if user.TotalPackets >= 100 {
		loss := float64(user.LostPackets) / float64(user.TotalPackets)
		user.LostPackets = 0
		user.TotalPackets = 0
		if loss > 0.05 {
			return "DOWN"
		} else if loss < 0.01 {
			return "UP"
		}
	}
	return ""
}

func (rm *RoomManager) Cleanup() {
	rm.Mu.Lock()
	defer rm.Mu.Unlock()
	now := time.Now()
	for roomID, users := range rm.Rooms {
		for userID, user := range users {
			if now.Sub(user.LastSeen) > 30*time.Second {
				delete(rm.AddrToUser, user.Conn.RemoteAddr().String())
				delete(rm.UserToRoom, userID)
				user.Conn.Close()
				delete(users, userID)
			}
		}
		if len(rm.Rooms[roomID]) == 0 {
			delete(rm.Rooms, roomID)
		}
	}
}
