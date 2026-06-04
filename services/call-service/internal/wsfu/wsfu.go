// Package wsfu implements a WebRTC SFU (Selective Forwarding Unit) that bridges
// browser WebRTC connections through the server. Each client sends a CALL_OFFER
// via WebSocket → gateway routes to Redis "callsfu:signal" → SFU answers with
// CALL_ANSWER → ICE exchange → SFU forwards RTP audio between participants.
package wsfu

import (
	"context"
	"encoding/json"
	"log"
	"sync"

	"github.com/pion/rtp"
	"github.com/pion/webrtc/v3"
	"github.com/redis/go-redis/v9"
)

type peer struct {
	pc        *webrtc.PeerConnection
	sendTrack *webrtc.TrackLocalStaticRTP // SFU writes other peers' audio here → sent to this client
	userID    string
	chatID    string
}

// SFU manages WebRTC peer connections and forwards audio between participants.
type SFU struct {
	mu    sync.RWMutex
	rooms map[string]map[string]*peer // chatID → userID → peer
	api   *webrtc.API
	rdb   *redis.Client
}

func New(rdb *redis.Client) *SFU {
	m := &webrtc.MediaEngine{}
	if err := m.RegisterDefaultCodecs(); err != nil {
		log.Fatalf("[SFU] RegisterDefaultCodecs: %v", err)
	}
	return &SFU{
		rooms: make(map[string]map[string]*peer),
		api:   webrtc.NewAPI(webrtc.WithMediaEngine(m)),
		rdb:   rdb,
	}
}

// Run subscribes to Redis "callsfu:signal" and processes WebRTC signaling forever.
func (s *SFU) Run(ctx context.Context) {
	sub := s.rdb.Subscribe(ctx, "callsfu:signal")
	defer sub.Close()
	log.Println("[SFU] listening on callsfu:signal")
	for msg := range sub.Channel() {
		var data map[string]string
		if err := json.Unmarshal([]byte(msg.Payload), &data); err != nil {
			continue
		}
		switch data["type"] {
		case "CALL_OFFER":
			go s.handleOffer(ctx, data["chat_id"], data["user_id"], data["sdp"])
		case "CALL_ICE":
			go s.handleICE(data["chat_id"], data["user_id"], data["candidate"])
		}
	}
}

func (s *SFU) handleOffer(ctx context.Context, chatID, userID, sdpJSON string) {
	var offer webrtc.SessionDescription
	if err := json.Unmarshal([]byte(sdpJSON), &offer); err != nil {
		log.Printf("[SFU] bad sdp from %s: %v", userID, err)
		return
	}

	// Track that SFU sends TO this client (carries audio from the other participant).
	sendTrack, err := webrtc.NewTrackLocalStaticRTP(
		webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeOpus},
		"audio", "sfu-"+userID,
	)
	if err != nil {
		log.Printf("[SFU] NewTrackLocalStaticRTP: %v", err)
		return
	}

	pc, err := s.api.NewPeerConnection(webrtc.Configuration{
		ICEServers: []webrtc.ICEServer{
			{URLs: []string{"stun:stun.l.google.com:19302"}},
		},
	})
	if err != nil {
		log.Printf("[SFU] NewPeerConnection: %v", err)
		return
	}

	if _, err := pc.AddTrack(sendTrack); err != nil {
		log.Printf("[SFU] AddTrack: %v", err)
		pc.Close()
		return
	}

	// When audio arrives FROM this client, forward to everyone else in the room.
	pc.OnTrack(func(remote *webrtc.TrackRemote, _ *webrtc.RTPReceiver) {
		log.Printf("[SFU] track from user=%s chat=%s codec=%s", userID, chatID, remote.Codec().MimeType)
		for {
			pkt, _, err := remote.ReadRTP()
			if err != nil {
				return
			}
			s.forwardRTP(chatID, userID, pkt)
		}
	})

	// Send ICE candidates (SFU side) to the client via ws:user:<userID>.
	pc.OnICECandidate(func(c *webrtc.ICECandidate) {
		if c == nil {
			return
		}
		candJSON, _ := json.Marshal(c.ToJSON())
		payload, _ := json.Marshal(map[string]string{
			"type":      "CALL_ICE",
			"chat_id":   chatID,
			"candidate": string(candJSON),
		})
		s.rdb.Publish(ctx, "ws:user:"+userID, string(payload))
	})

	pc.OnConnectionStateChange(func(state webrtc.PeerConnectionState) {
		log.Printf("[SFU] user=%s chat=%s state→%s", userID, chatID, state)
		if state == webrtc.PeerConnectionStateFailed ||
			state == webrtc.PeerConnectionStateClosed ||
			state == webrtc.PeerConnectionStateDisconnected {
			s.removePeer(chatID, userID)
		}
	})

	if err := pc.SetRemoteDescription(offer); err != nil {
		log.Printf("[SFU] SetRemoteDescription: %v", err)
		pc.Close()
		return
	}
	answer, err := pc.CreateAnswer(nil)
	if err != nil {
		log.Printf("[SFU] CreateAnswer: %v", err)
		pc.Close()
		return
	}
	if err := pc.SetLocalDescription(answer); err != nil {
		log.Printf("[SFU] SetLocalDescription: %v", err)
		pc.Close()
		return
	}

	// Register peer before sending answer (so forwarding works immediately).
	s.mu.Lock()
	if s.rooms[chatID] == nil {
		s.rooms[chatID] = make(map[string]*peer)
	}
	if old, ok := s.rooms[chatID][userID]; ok {
		old.pc.Close()
	}
	s.rooms[chatID][userID] = &peer{pc: pc, sendTrack: sendTrack, userID: userID, chatID: chatID}
	s.mu.Unlock()

	// Send the answer to the client (trickle ICE: candidates follow via OnICECandidate).
	answerJSON, _ := json.Marshal(pc.LocalDescription())
	payload, _ := json.Marshal(map[string]string{
		"type":    "CALL_ANSWER",
		"chat_id": chatID,
		"sdp":     string(answerJSON),
	})
	s.rdb.Publish(ctx, "ws:user:"+userID, string(payload))
	log.Printf("[SFU] answered user=%s chat=%s", userID, chatID)
}

func (s *SFU) handleICE(chatID, userID, candidateJSON string) {
	s.mu.RLock()
	room := s.rooms[chatID]
	var p *peer
	if room != nil {
		p = room[userID]
	}
	s.mu.RUnlock()
	if p == nil {
		return
	}
	var cand webrtc.ICECandidateInit
	if err := json.Unmarshal([]byte(candidateJSON), &cand); err != nil {
		return
	}
	if err := p.pc.AddICECandidate(cand); err != nil {
		log.Printf("[SFU] AddICECandidate user=%s: %v", userID, err)
	}
}

// forwardRTP writes an RTP packet from senderID to all other peers in the room.
// pion rewrites SSRC and PayloadType automatically on WriteRTP.
func (s *SFU) forwardRTP(chatID, senderID string, pkt *rtp.Packet) {
	s.mu.RLock()
	room := s.rooms[chatID]
	s.mu.RUnlock()
	for uid, p := range room {
		if uid == senderID {
			continue
		}
		out := *pkt // shallow copy so SSRC rewrite per-binding doesn't race
		if err := p.sendTrack.WriteRTP(&out); err != nil {
			log.Printf("[SFU] forward→%s: %v", uid, err)
		}
	}
}

func (s *SFU) removePeer(chatID, userID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	room := s.rooms[chatID]
	if room == nil {
		return
	}
	if p, ok := room[userID]; ok {
		p.pc.Close()
		delete(room, userID)
		log.Printf("[SFU] removed user=%s chat=%s", userID, chatID)
	}
	if len(room) == 0 {
		delete(s.rooms, chatID)
	}
}
