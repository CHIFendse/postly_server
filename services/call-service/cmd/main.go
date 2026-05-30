package main

import (
	"crypto/tls"
	"encoding/binary"
	"errors"
	"log"
	"net"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/joho/godotenv"
	dtls "github.com/pion/dtls/v2"
	"golang.org/x/time/rate"

	"call-service/internal/sfu"
	"call-service/internal/udp"
)

var (
	udpLimiter   = make(map[string]*rate.Limiter)
	udpLimiterMu sync.Mutex
)

type claims struct {
	UserID string `json:"user_id"`
	jwt.RegisteredClaims
}

func main() {
	if err := godotenv.Load(); err != nil {
		log.Fatal("cannot load .env")
	}

	rooms := udp.NewRoomManager()

	go func() {
		ticker := time.NewTicker(30 * time.Second)
		for range ticker.C {
			rooms.Cleanup()
		}
	}()

	cert, err := tls.LoadX509KeyPair(os.Getenv("TLS_CERT"), os.Getenv("TLS_KEY"))
	if err != nil {
		log.Fatalf("TLS: %v", err)
	}

	dtlsConfig := &dtls.Config{
		Certificates: []tls.Certificate{cert},
		ClientAuth:   dtls.NoClientCert,
	}

	port := getenv("DTLS_PORT", "8082")
	addr := &net.UDPAddr{IP: net.IPv4(0, 0, 0, 0), Port: mustPort(port)}
	lis, err := dtls.Listen("udp4", addr, dtlsConfig)
	if err != nil {
		log.Fatalf("DTLS listen: %v", err)
	}
	defer lis.Close()
	log.Printf("call-service listening on :%s (DTLS)", port)

	for {
		conn, err := lis.Accept()
		if err != nil {
			log.Printf("[DTLS] accept: %v", err)
			continue
		}
		go handleConn(conn, rooms)
	}
}

func handleConn(conn net.Conn, rooms *udp.RoomManager) {
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
		if !getLimiter(ip).Allow() {
			continue
		}

		msg := string(buf[:n])
		if strings.HasPrefix(msg, "HELLO ") {
			parts := strings.Split(msg, " ")
			if len(parts) < 3 {
				continue
			}
			uid, err := validateToken(parts[1])
			if err != nil {
				log.Printf("[DTLS] bad token %s: %v", ipStr, err)
				continue
			}
			rooms.AddUser(parts[2], uid, conn)
			continue
		}
		if strings.HasPrefix(msg, "BYE") {
			rooms.RemoveUserByAddr(ipStr)
			return
		}

		if participants, ok := rooms.GetParticipants(ipStr); ok {
			rooms.UpdateActivity(ipStr)
			if n >= 4 {
				seq := binary.BigEndian.Uint32(buf[:4])
				switch rooms.AnalyzePacketLoss(ipStr, seq) {
				case "DOWN":
					conn.Write([]byte("QUALITY_DOWN"))
				case "UP":
					conn.Write([]byte("QUALITY_UP"))
				}
			}
			sfu.Forward(buf[:n], participants, conn)
		}
	}
}

func validateToken(raw string) (string, error) {
	tok, err := jwt.ParseWithClaims(raw, &claims{}, func(*jwt.Token) (any, error) {
		return []byte(os.Getenv("JWT_SECRET")), nil
	})
	if err != nil || !tok.Valid {
		return "", errors.New("invalid token")
	}
	if c, ok := tok.Claims.(*claims); ok {
		return c.UserID, nil
	}
	return "", errors.New("invalid claims")
}

func getLimiter(ip string) *rate.Limiter {
	udpLimiterMu.Lock()
	defer udpLimiterMu.Unlock()
	if l, ok := udpLimiter[ip]; ok {
		return l
	}
	l := rate.NewLimiter(100, 200)
	udpLimiter[ip] = l
	return l
}

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func mustPort(s string) int {
	var p int
	if _, err := fmt.Sscanf(s, "%d", &p); err != nil {
		log.Fatalf("invalid port: %s", s)
	}
	return p
}
