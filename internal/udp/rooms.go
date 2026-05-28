package udp

import (
	"net"
	"sync"
	"time"
	"log"
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
	Rooms      map[string]map[string]*UserAddr // map[roomID]map[userID]*UserAddr
	AddrToUser map[string]string               // map[ip:port]userID
	UserToRoom map[string]string               // map[userID]roomID
}

func NewRoomManager() *RoomManager {
	return &RoomManager{
		Rooms:      make(map[string]map[string]*UserAddr),
		AddrToUser: make(map[string]string),
		UserToRoom: make(map[string]string),
	}
}

// Returns true if this is a new user (for logging).
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

	rm.Rooms[roomID][userID] = &UserAddr{
		Conn:     conn,
		LastSeen: time.Now(),
	}
	rm.AddrToUser[addrStr] = userID
	rm.UserToRoom[userID] = roomID
	log.Printf("[DEBUG] user registered with address %s", addrStr)

	return isNew
}

func (rm *RoomManager) GetParticipants(addrStr string) ([]net.Conn, bool) {
	rm.Mu.RLock()
	defer rm.Mu.RUnlock()

	userID, ok := rm.AddrToUser[addrStr]
	if !ok {
		log.Printf("[DEBUG] GetParticipants: address %s not found in AddrToUser", addrStr)
		return nil, false
	}

	roomID, ok := rm.UserToRoom[userID]
	if !ok {
		log.Printf("[DEBUG] GetParticipants: user %s not in any room", userID)
		return nil, false
	}

	users := rm.Rooms[roomID]
	conns := make([]net.Conn, 0, len(users))
	for _, user := range users {
		conns = append(conns, user.Conn)
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
	log.Printf("[DTLS] user %s left the chat", userID)

	delete(rm.AddrToUser, addrStr)
	delete(rm.UserToRoom, userID)
	if room, ok := rm.Rooms[roomID]; ok {
		delete(room, userID)
	}
}

func (rm *RoomManager) AnalyzePacketLoss(addrStr string, currentSeq uint32) string {
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

	if user.LastSeq != 0 && currentSeq > user.LastSeq+1 {
		user.LostPackets += int(currentSeq - user.LastSeq - 1)
	}
	user.LastSeq = currentSeq

	if user.TotalPackets >= 100 {
		lossRate := float64(user.LostPackets) / float64(user.TotalPackets)
		lp := user.LostPackets
		user.LostPackets = 0
		user.TotalPackets = 0

		if lossRate > 0.05 {
			log.Printf("[RTCP] high losses %s: %.2f%% (%d pac.)", userID, lossRate*100, lp)
			return "DOWN"
		} else if lossRate < 0.01 {
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
				log.Printf("[DTLS] timeout user %s", userID)
				addrStr := user.Conn.RemoteAddr().String()
				user.Conn.Close()
				delete(rm.AddrToUser, addrStr)
				delete(rm.UserToRoom, userID)
				delete(users, userID)
			}
		}
		if len(rm.Rooms[roomID]) == 0 {
			delete(rm.Rooms, roomID)
		}
	}
}
