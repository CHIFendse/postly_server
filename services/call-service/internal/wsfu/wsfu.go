package wsfu

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"maps"
	"net"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/pion/webrtc/v3"
	"github.com/redis/go-redis/v9"
)

const maxPeersPerRoom = 16

type peer struct {
	userID string
	pc     *webrtc.PeerConnection

	mu         sync.Mutex
	needsOffer bool
	pendingICE []webrtc.ICECandidateInit
}

type room struct {
	peers  map[string]*peer
	tracks map[string]*webrtc.TrackLocalStaticRTP
}

type SFU struct {
	mu    sync.Mutex
	rooms map[string]*room
	api   *webrtc.API
	rdb   *redis.Client
	ctx   context.Context
	udp   *net.UDPConn
	tcp   *net.TCPListener
}

func New(ctx context.Context, rdb *redis.Client) *SFU {
	m := &webrtc.MediaEngine{}
	if err := m.RegisterDefaultCodecs(); err != nil {
		log.Fatalf("[SFU] RegisterDefaultCodecs: %v", err)
	}

	publicIP, err := resolvePublicIP(os.Getenv("WEBRTC_PUBLIC_IP"))
	if err != nil {
		log.Fatalf("[SFU] WEBRTC_PUBLIC_IP: %v", err)
	}
	port := envPort("WEBRTC_PORT", 10000)

	udp, err := net.ListenUDP("udp4", &net.UDPAddr{Port: port})
	if err != nil {
		log.Fatalf("[SFU] listen udp :%d: %v", port, err)
	}
	tcp, err := net.ListenTCP("tcp4", &net.TCPAddr{Port: port})
	if err != nil {
		log.Fatalf("[SFU] listen tcp :%d: %v", port, err)
	}

	se := webrtc.SettingEngine{}
	se.SetICEUDPMux(webrtc.NewICEUDPMux(nil, udp))
	se.SetICETCPMux(webrtc.NewICETCPMux(nil, tcp, 64))
	se.SetNetworkTypes([]webrtc.NetworkType{webrtc.NetworkTypeUDP4, webrtc.NetworkTypeTCP4})
	se.SetNAT1To1IPs([]string{publicIP}, webrtc.ICECandidateTypeHost)
	log.Printf("[SFU] public ip=%s port=%d (udp+tcp)", publicIP, port)

	return &SFU{
		rooms: make(map[string]*room),
		api:   webrtc.NewAPI(webrtc.WithMediaEngine(m), webrtc.WithSettingEngine(se)),
		rdb:   rdb,
		ctx:   ctx,
		udp:   udp,
		tcp:   tcp,
	}
}

func resolvePublicIP(value string) (string, error) {
	if value == "" {
		return "", errors.New("is required")
	}
	if ip := net.ParseIP(value); ip != nil {
		if ip.To4() == nil {
			return "", fmt.Errorf("%s is not an IPv4 address", value)
		}
		return ip.String(), nil
	}
	ips, err := net.LookupIP(value)
	if err != nil {
		return "", fmt.Errorf("resolve %s: %w", value, err)
	}
	for _, ip := range ips {
		if v4 := ip.To4(); v4 != nil {
			log.Printf("[SFU] %s resolved to %s", value, v4)
			return v4.String(), nil
		}
	}
	return "", fmt.Errorf("%s has no IPv4 address", value)
}

func envPort(key string, fallback int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.ParseUint(v, 10, 16); err == nil && n > 0 {
			return int(n)
		}
		log.Fatalf("[SFU] invalid %s=%q", key, v)
	}
	return fallback
}

func (s *SFU) Run() {
	sub := s.rdb.Subscribe(s.ctx, "callsfu:signal")
	defer sub.Close()
	ch := sub.Channel()
	log.Println("[SFU] listening on callsfu:signal")

	for {
		select {
		case <-s.ctx.Done():
			return
		case msg, ok := <-ch:
			if !ok {
				return
			}
			var d map[string]string
			if err := json.Unmarshal([]byte(msg.Payload), &d); err != nil {
				continue
			}
			chatID, userID := d["chat_id"], d["user_id"]
			if chatID == "" || userID == "" {
				continue
			}
			switch d["type"] {
			case "CALL_JOIN":
				s.join(chatID, userID)
			case "CALL_ANSWER":
				s.answer(chatID, userID, d["sdp"])
			case "CALL_ICE":
				s.ice(chatID, userID, d["candidate"])
			case "CALL_HANGUP":
				s.leave(chatID, userID)
			}
		}
	}
}

func (s *SFU) join(chatID, userID string) {
	s.leave(chatID, userID)

	s.mu.Lock()
	full := s.rooms[chatID] != nil && len(s.rooms[chatID].peers) >= maxPeersPerRoom
	s.mu.Unlock()
	if full {
		s.send(userID, map[string]string{"type": "CALL_ERROR", "chat_id": chatID, "error": "Комната заполнена"})
		return
	}

	pc, err := s.api.NewPeerConnection(webrtc.Configuration{})
	if err != nil {
		log.Printf("[SFU] NewPeerConnection: %v", err)
		return
	}
	if _, err := pc.AddTransceiverFromKind(webrtc.RTPCodecTypeAudio, webrtc.RTPTransceiverInit{
		Direction: webrtc.RTPTransceiverDirectionRecvonly,
	}); err != nil {
		log.Printf("[SFU] AddTransceiver: %v", err)
		pc.Close()
		return
	}

	p := &peer{userID: userID, pc: pc}

	pc.OnTrack(func(remote *webrtc.TrackRemote, _ *webrtc.RTPReceiver) {
		s.relay(chatID, userID, remote)
	})
	pc.OnConnectionStateChange(func(st webrtc.PeerConnectionState) {
		log.Printf("[SFU] user=%s chat=%s state=%s", userID, chatID, st)
		if st == webrtc.PeerConnectionStateFailed || st == webrtc.PeerConnectionStateClosed {
			s.removePeer(chatID, userID, pc)
		}
	})

	s.mu.Lock()
	r := s.rooms[chatID]
	if r == nil {
		r = &room{peers: map[string]*peer{}, tracks: map[string]*webrtc.TrackLocalStaticRTP{}}
		s.rooms[chatID] = r
	}
	r.peers[userID] = p
	s.mu.Unlock()

	log.Printf("[SFU] joined user=%s chat=%s", userID, chatID)
	s.negotiate(chatID, p)
	s.broadcastPeers(chatID)
}

func (s *SFU) relay(chatID, userID string, remote *webrtc.TrackRemote) {
	if remote.Kind() != webrtc.RTPCodecTypeAudio {
		return
	}
	local, err := webrtc.NewTrackLocalStaticRTP(remote.Codec().RTPCodecCapability, "audio-"+userID, "user-"+userID)
	if err != nil {
		log.Printf("[SFU] NewTrackLocalStaticRTP: %v", err)
		return
	}

	s.mu.Lock()
	r := s.rooms[chatID]
	if r == nil {
		s.mu.Unlock()
		return
	}
	r.tracks[userID] = local
	s.mu.Unlock()
	s.renegotiateAll(chatID)

	defer func() {
		s.mu.Lock()
		if r := s.rooms[chatID]; r != nil && r.tracks[userID] == local {
			delete(r.tracks, userID)
		}
		s.mu.Unlock()
		s.renegotiateAll(chatID)
	}()

	buf := make([]byte, 1500)
	for {
		n, _, err := remote.Read(buf)
		if err != nil {
			return
		}
		if _, err := local.Write(buf[:n]); err != nil && !errors.Is(err, io.ErrClosedPipe) {
			return
		}
	}
}

func (s *SFU) negotiate(chatID string, p *peer) {
	s.mu.Lock()
	var tracks map[string]*webrtc.TrackLocalStaticRTP
	if r := s.rooms[chatID]; r != nil {
		tracks = maps.Clone(r.tracks)
	}
	s.mu.Unlock()

	p.mu.Lock()
	defer p.mu.Unlock()
	if p.pc.ConnectionState() == webrtc.PeerConnectionStateClosed {
		return
	}

	sending := map[string]bool{}
	for _, sender := range p.pc.GetSenders() {
		t := sender.Track()
		if t == nil {
			continue
		}
		owner := strings.TrimPrefix(t.StreamID(), "user-")
		if cur, ok := tracks[owner]; !ok || webrtc.TrackLocal(cur) != t {
			if err := p.pc.RemoveTrack(sender); err != nil {
				log.Printf("[SFU] RemoveTrack user=%s: %v", p.userID, err)
			}
			continue
		}
		sending[owner] = true
	}
	for owner, t := range tracks {
		if owner == p.userID || sending[owner] {
			continue
		}
		sender, err := p.pc.AddTrack(t)
		if err != nil {
			log.Printf("[SFU] AddTrack user=%s: %v", p.userID, err)
			continue
		}
		go drainRTCP(sender)
	}

	if p.pc.SignalingState() != webrtc.SignalingStateStable {
		p.needsOffer = true
		return
	}
	p.needsOffer = false

	offer, err := p.pc.CreateOffer(nil)
	if err != nil {
		log.Printf("[SFU] CreateOffer user=%s: %v", p.userID, err)
		return
	}
	gathered := webrtc.GatheringCompletePromise(p.pc)
	if err := p.pc.SetLocalDescription(offer); err != nil {
		log.Printf("[SFU] SetLocalDescription user=%s: %v", p.userID, err)
		return
	}
	select {
	case <-gathered:
	case <-time.After(3 * time.Second):
		log.Printf("[SFU] ICE gathering timeout user=%s", p.userID)
	}

	sdp, _ := json.Marshal(p.pc.LocalDescription())
	s.send(p.userID, map[string]string{"type": "CALL_OFFER", "chat_id": chatID, "sdp": string(sdp)})
}

func (s *SFU) answer(chatID, userID, sdpJSON string) {
	p := s.peer(chatID, userID)
	if p == nil {
		return
	}
	var ans webrtc.SessionDescription
	if err := json.Unmarshal([]byte(sdpJSON), &ans); err != nil {
		log.Printf("[SFU] bad answer from %s: %v", userID, err)
		return
	}

	p.mu.Lock()
	if err := p.pc.SetRemoteDescription(ans); err != nil {
		p.mu.Unlock()
		log.Printf("[SFU] SetRemoteDescription user=%s: %v", userID, err)
		return
	}
	for _, c := range p.pendingICE {
		if err := p.pc.AddICECandidate(c); err != nil {
			log.Printf("[SFU] AddICECandidate (buffered) user=%s: %v", userID, err)
		}
	}
	p.pendingICE = nil
	again := p.needsOffer
	p.mu.Unlock()

	if again {
		s.negotiate(chatID, p)
	}
}

func (s *SFU) ice(chatID, userID, candJSON string) {
	p := s.peer(chatID, userID)
	if p == nil {
		return
	}
	var c webrtc.ICECandidateInit
	if err := json.Unmarshal([]byte(candJSON), &c); err != nil {
		return
	}

	p.mu.Lock()
	defer p.mu.Unlock()
	if p.pc.RemoteDescription() == nil {
		if len(p.pendingICE) < 50 {
			p.pendingICE = append(p.pendingICE, c)
		}
		return
	}
	if err := p.pc.AddICECandidate(c); err != nil {
		log.Printf("[SFU] AddICECandidate user=%s: %v", userID, err)
	}
}

func (s *SFU) leave(chatID, userID string) {
	if p := s.peer(chatID, userID); p != nil {
		s.removePeer(chatID, userID, p.pc)
	}
}

func (s *SFU) removePeer(chatID, userID string, pc *webrtc.PeerConnection) {
	s.mu.Lock()
	removed := false
	if r := s.rooms[chatID]; r != nil {
		if p := r.peers[userID]; p != nil && p.pc == pc {
			delete(r.peers, userID)
			removed = true
			if len(r.peers) == 0 {
				delete(s.rooms, chatID)
			}
		}
	}
	s.mu.Unlock()

	pc.Close()
	if removed {
		log.Printf("[SFU] removed user=%s chat=%s", userID, chatID)
		s.renegotiateAll(chatID)
		s.broadcastPeers(chatID)
	}
}

func (s *SFU) renegotiateAll(chatID string) {
	s.mu.Lock()
	var peers []*peer
	if r := s.rooms[chatID]; r != nil {
		for _, p := range r.peers {
			peers = append(peers, p)
		}
	}
	s.mu.Unlock()

	for _, p := range peers {
		s.negotiate(chatID, p)
	}
}

func (s *SFU) broadcastPeers(chatID string) {
	s.mu.Lock()
	var ids []string
	if r := s.rooms[chatID]; r != nil {
		for id := range r.peers {
			ids = append(ids, id)
		}
	}
	s.mu.Unlock()

	msg := map[string]string{"type": "CALL_PEERS", "chat_id": chatID, "user_ids": strings.Join(ids, ",")}
	for _, id := range ids {
		s.send(id, msg)
	}
}

func (s *SFU) peer(chatID, userID string) *peer {
	s.mu.Lock()
	defer s.mu.Unlock()
	if r := s.rooms[chatID]; r != nil {
		return r.peers[userID]
	}
	return nil
}

func (s *SFU) send(userID string, msg map[string]string) {
	b, _ := json.Marshal(msg)
	if err := s.rdb.Publish(s.ctx, "ws:user:"+userID, b).Err(); err != nil {
		log.Printf("[SFU] publish to %s: %v", userID, err)
	}
}

func drainRTCP(sender *webrtc.RTPSender) {
	buf := make([]byte, 1500)
	for {
		if _, _, err := sender.Read(buf); err != nil {
			return
		}
	}
}

func (s *SFU) Close() {
	s.mu.Lock()
	var pcs []*webrtc.PeerConnection
	for _, r := range s.rooms {
		for _, p := range r.peers {
			pcs = append(pcs, p.pc)
		}
	}
	s.rooms = map[string]*room{}
	s.mu.Unlock()

	for _, pc := range pcs {
		pc.Close()
	}
	s.udp.Close()
	s.tcp.Close()
}
