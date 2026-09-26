package router

import (
	"io"
	"log"
	"net/http"
	"net/url"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"gateway-service/internal/clients"
	chatpb "postly/proto/chat"
	msgpb "postly/proto/messaging"
	s3pb "postly/proto/s3"
)

// Файлы сообщений отдаются только через gateway: presigned-ссылка на S3
// открывается у любого, у кого она есть, поэтому клиенту она не выдаётся.

// fileURLFor — адрес файла сообщения для клиента (запрашивать с Authorization)
func fileURLFor(messageID string) string {
	return "/file?message_id=" + url.QueryEscape(messageID)
}

// Заголовки, которые пробрасываем из S3 клиенту
var passFileHeaders = []string{
	"Content-Type", "Content-Length", "Content-Range", "Accept-Ranges",
	"ETag", "Last-Modified",
}

// Заголовки запроса, которые пробрасываем в S3 (перемотка видео, кэш)
var passRangeHeaders = []string{
	"Range", "If-Range", "If-None-Match", "If-Modified-Since",
}

func RegisterFiles(mux *http.ServeMux, c *clients.Clients) {
	// Без JWTMiddleware: его 10-секундный таймаут оборвал бы отдачу видео
	mux.HandleFunc("/file", handleFile(c))
}

func handleFile(c *clients.Clients) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		userID, ok := authenticate(c, r)
		if !ok {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		messageID := r.URL.Query().Get("message_id")
		if messageID == "" {
			http.Error(w, "message_id required", http.StatusBadRequest)
			return
		}

		info, err := c.Messaging.GetFileInfo(r.Context(), &msgpb.GetFileInfoRequest{MessageId: messageID})
		if status.Code(err) == codes.NotFound {
			http.Error(w, "Not found", http.StatusNotFound)
			return
		}
		if err != nil {
			log.Printf("[GW] file: GetFileInfo msg=%s: %v", messageID, err)
			http.Error(w, "Internal error", http.StatusInternalServerError)
			return
		}

		pts, err := c.Chat.GetParticipants(r.Context(), &chatpb.GetParticipantsRequest{ChatId: info.ChatId})
		if err != nil {
			log.Printf("[GW] file: GetParticipants chat=%s: %v", info.ChatId, err)
			http.Error(w, "Internal error", http.StatusInternalServerError)
			return
		}
		member := false
		for _, uid := range pts.UserIds {
			if uid == userID {
				member = true
				break
			}
		}
		// 404, а не 403 — не подтверждаем существование чужих сообщений
		if !member {
			http.Error(w, "Not found", http.StatusNotFound)
			return
		}

		// Короткая ссылка — используется только здесь, клиенту не уходит
		urlResp, err := c.S3.GetDownloadUrl(r.Context(), &s3pb.GetDownloadUrlRequest{
			S3Key:        info.S3Key,
			ExpiresInSec: 60,
		})
		if err != nil {
			log.Printf("[GW] file: GetDownloadUrl msg=%s: %v", messageID, err)
			http.Error(w, "Internal error", http.StatusInternalServerError)
			return
		}

		// Всегда GET: подпись выдана под GET, на HEAD просто не пишем тело
		s3Req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, urlResp.Url, nil)
		if err != nil {
			http.Error(w, "Internal error", http.StatusInternalServerError)
			return
		}
		for _, h := range passRangeHeaders {
			if v := r.Header.Get(h); v != "" {
				s3Req.Header.Set(h, v)
			}
		}

		s3Resp, err := http.DefaultClient.Do(s3Req)
		if err != nil {
			log.Printf("[GW] file: S3 GET msg=%s: %v", messageID, err)
			http.Error(w, "Bad gateway", http.StatusBadGateway)
			return
		}
		defer s3Resp.Body.Close()

		switch s3Resp.StatusCode {
		case http.StatusOK, http.StatusPartialContent, http.StatusNotModified,
			http.StatusRequestedRangeNotSatisfiable:
		case http.StatusNotFound:
			http.Error(w, "Not found", http.StatusNotFound)
			return
		default:
			log.Printf("[GW] file: S3 status %d msg=%s", s3Resp.StatusCode, messageID)
			http.Error(w, "Bad gateway", http.StatusBadGateway)
			return
		}

		for _, h := range passFileHeaders {
			if v := s3Resp.Header.Get(h); v != "" {
				w.Header().Set(h, v)
			}
		}
		// Content-Type задаёт клиент при загрузке — не даём ему исполниться
		// как страница на домене gateway
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Content-Security-Policy", "sandbox; default-src 'none'")
		w.Header().Set("Cache-Control", "private, max-age=3600")
		if info.Type == "file" {
			w.Header().Set("Content-Disposition",
				"attachment; filename*=UTF-8''"+url.PathEscape(info.FileName))
		} else {
			w.Header().Set("Content-Disposition", "inline")
		}

		w.WriteHeader(s3Resp.StatusCode)
		if r.Method == http.MethodHead {
			return
		}
		if _, err := io.Copy(w, s3Resp.Body); err != nil {
			log.Printf("[GW] file: stream msg=%s: %v", messageID, err)
		}
	}
}
