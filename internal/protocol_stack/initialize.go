package protocol_stack

import (
	"fmt"
	"log"
	"net"
	"net/http"
	"strings"
    "sync"
    "backend/internal/database"
    "backend/internal"
    "time"
    "backend/internal/udp"
	"encoding/binary"

)


type Response struct {
    Status bool `json:"status"`
}

type UserAddr struct {
    Addr     *net.UDPAddr
    LastSeen time.Time
}

type IpAddresses struct {
    mu     sync.Mutex
    IpList map[string]*UserAddr
}

var authService *database.AuthService
var repo *database.Repository

func (s *IpAddresses) AddIP(addr *net.UDPAddr) {
    s.mu.Lock()
    defer s.mu.Unlock()
    s.IpList[addr.String()] = &UserAddr{
        Addr:     addr,
        LastSeen: time.Now(),
    }
}


func (s *IpAddresses) DelIP (ip string){
    s.mu.Lock()
    defer s.mu.Unlock()
    delete(s.IpList, ip)
}

func (s *IpAddresses) UpdateLastSeen(ip string) {
    s.mu.Lock()
    defer s.mu.Unlock()
    if user, exists := s.IpList[ip]; exists {
        user.LastSeen = time.Now()
    }
}

func (s *IpAddresses) IsAuthorized(ip string) bool {
    s.mu.Lock()
    defer s.mu.Unlock()
    _, exists := s.IpList[ip]
    return exists
}

func (s *IpAddresses) GetList() []*net.UDPAddr {
    s.mu.Lock()
    defer s.mu.Unlock()
    list := make([]*net.UDPAddr, 0, len(s.IpList))
    for _, user := range s.IpList {
        list = append(list, user.Addr)
    }
    return list
}


func Start() error {
    db, err := database.InitDB()
    if err != nil {
        return fmt.Errorf("failed to init db: %v", err)
    }
    authService = database.NewAuthService(db)
    repo = database.NewRepository(db)
    mux := http.NewServeMux()
    limiter := NewIPRateLimiter(5, 10)

    mux.HandleFunc("/", JWTMiddleware(handleHTTP))
    mux.HandleFunc("/register", handleRegister)
    mux.HandleFunc("/login", handleLogin)
    mux.HandleFunc("/verify", JWTMiddleware(handleVerify))
    mux.HandleFunc("/getMessages", JWTMiddleware(handleGetMessages))
    mux.HandleFunc("/getChats", JWTMiddleware(handleGetChats))
    mux.HandleFunc("/addMessage", JWTMiddleware(handleAddMessage))
    mux.HandleFunc("/createChat", JWTMiddleware(handleCreateChat))
    mux.HandleFunc("/ws", JWTMiddleware(handleWS))

    handlerWithCORS := enableCORS(mux)
    finalHandler := limitMiddleware(limiter, handlerWithCORS)
    return http.ListenAndServe(":8081", finalHandler)
}

func StartUDP() error {
    rooms := udp.NewRoomManager()

	go func() {
        ticker := time.NewTicker(30 * time.Second)
        for range ticker.C {
            rooms.Cleanup()
        }
    }()
	
    addr, _ := net.ResolveUDPAddr("udp", "0.0.0.0:8082")
    ln, _ := net.ListenUDP("udp", addr)
    defer ln.Close()
	ln.SetReadBuffer(4194304) 
	ln.SetWriteBuffer(4194304)
    buf := make([]byte, 2048)

    for {
        n, remoteAddr, err := ln.ReadFromUDP(buf)
        if err != nil {
            continue
        }
        if n < 3 {
			continue
		}
        ipStr := remoteAddr.String()
        if buf[0] == 'H' || buf[0] == 'B' {
            message := string(buf[:n])
        // А. ОБРАБОТКА ВХОДА (HELLO)
            if strings.HasPrefix(message, "HELLO ") {
                parts := strings.Split(message, " ")
                if len(parts) < 3 { continue }

                token, roomID := parts[1], parts[2]
                uid, err := authService.GetUserIDFromToken(token)
                if err != nil { 
                    log.Printf("[UDP] token error %s: %v", ipStr, err)
                    continue 
                }

                // РЕГИСТРАЦИЯ: без этого GetParticipants всегда будет возвращать false
                isNew := rooms.AddUser(roomID, uid, remoteAddr, ln)
                if isNew {
                    log.Printf("[UDP] user %s entered the room %s (%s)", uid, roomID, ipStr)
                }
                continue
            }

            // Б. ОБРАБОТКА ВЫХОДА (BYE)
            if strings.HasPrefix(message, "BYE") {
                rooms.RemoveUserByAddr(ipStr)
                continue
            }
        }
        // В. ОБРАБОТКА АУДИО
        // Теперь GetParticipants найдет участников, так как мы их добавили выше в HELLO
        if participants, ok := rooms.GetParticipants(ipStr); ok {
            rooms.UpdateActivity(ipStr)
            
            if n >= 4 {
                // Извлекаем Sequence Number для анализа потерь
                seq := binary.BigEndian.Uint32(buf[:4])
                action := rooms.AnalyzePacketLoss(ipStr, seq)
                
                if action == "DOWN" {
                    ln.WriteToUDP([]byte("QUALITY_DOWN"), remoteAddr)
                } else if action == "UP" {
                    ln.WriteToUDP([]byte("QUALITY_UP"), remoteAddr)
                }
            }

            // Рассылаем остальным
            internal.SFU(buf[:n], participants, remoteAddr, ln)
        } else {
            log.Printf("[UDP] packet from anonimous address: %s", ipStr)
        }
    }
}

