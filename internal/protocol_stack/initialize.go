package protocol_stack

import (
	"backend/internal"
	"backend/internal/database"
	"backend/internal/protocol_stack/handlers"
	"backend/internal/udp"
	"crypto/tls"
	"encoding/binary"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	dtls "github.com/pion/dtls/v2"
)

type Response struct {
	Status bool `json:"status"`
}

var authService *database.AuthService
var repo *database.Repository

func Start() error {
	db, err := database.InitDB()
	if err != nil {
		return fmt.Errorf("failed to init db: %v", err)
	}
	authService = database.NewAuthService(db)
	repo = database.NewRepository(db)

	mux := http.NewServeMux()
	mux.HandleFunc("/", handleHTTP)
	mux.HandleFunc("/register", handleRegister)
	mux.HandleFunc("/login", handleLogin)
	mux.HandleFunc("/getVersion", handlers.HandleVersion)
	mux.HandleFunc("/verify", JWTMiddleware(handleVerify))
	mux.HandleFunc("/getMessages", JWTMiddleware(handleGetMessages))
	mux.HandleFunc("/getChats", JWTMiddleware(handleGetChats))
	mux.HandleFunc("/getGroups", JWTMiddleware(handleGetGroups))
	mux.HandleFunc("/addMessage", JWTMiddleware(handleAddMessage))
	mux.HandleFunc("/createChat", JWTMiddleware(handleCreateChat))
	mux.HandleFunc("/createGroup", JWTMiddleware(handleCreateGroup))
	mux.HandleFunc("/ws", JWTMiddleware(handleWS))

	server := &http.Server{
		Addr:    "0.0.0.0:8081",
		Handler: limitMiddleware(NewIPRateLimiter(5, 10), enableCORS(mux)),
	}

	certFile := os.Getenv("TLS_CERT")
	keyFile := os.Getenv("TLS_KEY")

	tlsCert, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		return fmt.Errorf("failed to load TLS cert: %w", err)
	}

	// tcp4 to keep IPv4-only binding
	listener, err := tls.Listen("tcp4", "0.0.0.0:8081", &tls.Config{
		Certificates: []tls.Certificate{tlsCert},
	})
	if err != nil {
		return err
	}
	log.Println("[HTTPS] server started on :8081")
	return server.Serve(listener)
}

func StartUDP() error {
	rooms := udp.NewRoomManager()

	go func() {
		ticker := time.NewTicker(30 * time.Second)
		for range ticker.C {
			rooms.Cleanup()
		}
	}()

	certFile := os.Getenv("TLS_CERT")
	keyFile := os.Getenv("TLS_KEY")

	certificate, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		return fmt.Errorf("failed to load DTLS cert: %w", err)
	}

	dtlsConfig := &dtls.Config{
		Certificates: []tls.Certificate{certificate},
		ClientAuth:   dtls.NoClientCert,
	}

	addr := &net.UDPAddr{IP: net.IPv4(0, 0, 0, 0), Port: 8082}
	listener, err := dtls.Listen("udp4", addr, dtlsConfig)
	if err != nil {
		return fmt.Errorf("failed to listen DTLS: %w", err)
	}
	defer listener.Close()

	log.Println("[DTLS] server started on :8082")

	for {
		conn, err := listener.Accept()
		if err != nil {
			log.Printf("[DTLS] accept error: %v", err)
			continue
		}
		go handleDTLSConn(conn, rooms)
	}
}

func handleDTLSConn(conn net.Conn, rooms *udp.RoomManager) {
	defer conn.Close()
	ipStr := conn.RemoteAddr().String()
	buf := make([]byte, 2048)

	for {
		n, err := conn.Read(buf)
		if err != nil {
			rooms.RemoveUserByAddr(ipStr)
			return
		}
		if n < 3 {
			continue
		}

		ip, _, _ := net.SplitHostPort(ipStr)
		if !getUDPLimiter(ip).Allow() {
			continue
		}

		if buf[0] == 'H' || buf[0] == 'B' {
			message := string(buf[:n])
			if strings.HasPrefix(message, "HELLO ") {
				parts := strings.Split(message, " ")
				if len(parts) < 3 {
					continue
				}
				token, roomID := parts[1], parts[2]
				uid, err := authService.GetUserIDFromToken(token)
				if err != nil {
					log.Printf("[DTLS] token error %s: %v", ipStr, err)
					continue
				}
				isNew := rooms.AddUser(roomID, uid, conn)
				if isNew {
					log.Printf("[DTLS] user %s entered room %s (%s)", uid, roomID, ipStr)
				}
				continue
			}
			if strings.HasPrefix(message, "BYE") {
				rooms.RemoveUserByAddr(ipStr)
				return
			}
		}

		if participants, ok := rooms.GetParticipants(ipStr); ok {
			rooms.UpdateActivity(ipStr)

			if n >= 4 {
				seq := binary.BigEndian.Uint32(buf[:4])
				action := rooms.AnalyzePacketLoss(ipStr, seq)
				if action == "DOWN" {
					conn.Write([]byte("QUALITY_DOWN"))
				} else if action == "UP" {
					conn.Write([]byte("QUALITY_UP"))
				}
			}

			internal.SFU(buf[:n], participants, conn)
		} else {
			log.Printf("[DTLS] packet from anonymous: %s", ipStr)
		}
	}
}
