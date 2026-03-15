package udp

import (
	"net"
	"sync"
	"time"
	"log"
)

type UserAddr struct {
    mu           sync.Mutex   // ДОБАВЛЕНО: Индивидуальный мьютекс юзера
    Addr         *net.UDPAddr
    LastSeen     time.Time
    LastSeq      uint32
    LostPackets  int
    TotalPackets int
}
type RoomManager struct {
	Mu    sync.RWMutex
	Rooms map[string]map[string]*UserAddr // map[roomID]map[ip:port]*UserAddr
	// Обратный индекс для быстрого поиска комнаты по адресу
	AddrToUser map[string]string // map[ip:port]roomID
	UserToRoom map[string]string
}

func NewRoomManager() *RoomManager {
	return &RoomManager{
		Rooms:      make(map[string]map[string]*UserAddr),
		AddrToUser: make(map[string]string),
		UserToRoom: make(map[string]string),
	}
}

// Возвращает true, если это новый вход (для логов)
func (rm *RoomManager) AddUser(roomID, userID string, addr *net.UDPAddr, ln *net.UDPConn) bool {
    rm.Mu.Lock()
    defer rm.Mu.Unlock()

    isNew := false
    
    // Проверка смены адреса для того же ID (перезаход с другого порта/сети)
    if oldRoomID, exists := rm.UserToRoom[userID]; exists {
        if oldUser, ok := rm.Rooms[oldRoomID][userID]; ok {
            if oldUser.Addr.String() != addr.String() {
                delete(rm.AddrToUser, oldUser.Addr.String())
                // Можно послать KICK старому адресу, если нужно
            }
        }
    } else {
        isNew = true
    }

    if _, ok := rm.Rooms[roomID]; !ok {
        rm.Rooms[roomID] = make(map[string]*UserAddr)
    }

    rm.Rooms[roomID][userID] = &UserAddr{
        Addr:     addr,
        LastSeen: time.Now(),
    }
    rm.AddrToUser[addr.String()] = userID
    rm.UserToRoom[userID] = roomID
	log.Printf("[DEBUG] user registered with address %s", addr.String())
    
    return isNew
}

func (rm *RoomManager) GetParticipants(addrStr string) ([]*net.UDPAddr, bool) {
    rm.Mu.RLock()
    defer rm.Mu.RUnlock()

    // 1. Сначала узнаем, кто это по адресу
    userID, ok := rm.AddrToUser[addrStr]
    if !ok {
        // ЛОГ ЗДЕСЬ: Если это сработает, значит адрес изменился после HELLO
        log.Printf("[DEBUG] GetParticipants: address %s not found in AddrToUser", addrStr)
        return nil, false
    }

    // 2. Узнаем, в какой комнате этот пользователь
    roomID, ok := rm.UserToRoom[userID]
    if !ok {
        log.Printf("[DEBUG] GetParticipants: address %s not in the room", userID)
        return nil, false
    }

    // 3. Собираем адреса всех в этой комнате
    users := rm.Rooms[roomID]
    participants := make([]*net.UDPAddr, 0, len(users))
    for _, user := range users {
        participants = append(participants, user.Addr)
    }
    return participants, true
}

func (rm *RoomManager) UpdateActivity(addrStr string) {
    rm.Mu.RLock() // Достаточно RLock для поиска
    userID, ok := rm.AddrToUser[addrStr]
    if !ok {
        rm.Mu.RUnlock()
        return
    }
    roomID := rm.UserToRoom[userID]
    user := rm.Rooms[roomID][userID]
    rm.Mu.RUnlock()

    user.mu.Lock() // Блокируем только юзера
    user.LastSeen = time.Now()
    user.mu.Unlock()
}

func (rm *RoomManager) RemoveUserByAddr(addrStr string) {
    rm.Mu.Lock()
    defer rm.Mu.Unlock()

    userID, ok := rm.AddrToUser[addrStr]
    if !ok { return }

    roomID := rm.UserToRoom[userID]
    
    log.Printf("[UDP] user %s left the chat (BYE)", userID)
    
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
                log.Printf("[UDP] timeout user %s", userID)
                delete(rm.AddrToUser, user.Addr.String())
                delete(rm.UserToRoom, userID)
                delete(users, userID)
            }
        }
        if len(rm.Rooms[roomID]) == 0 {
            delete(rm.Rooms, roomID)
        }
    }
}