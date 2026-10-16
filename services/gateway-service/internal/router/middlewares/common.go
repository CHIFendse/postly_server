package middlewares

import (
	"net"
	"net/http"
	"net/netip"
	"bufio"
)

type ContextKey string

const UserIDKey ContextKey = "user_id"
const RequestIDKey ContextKey = "request_id"

var _ http.Flusher = (*StatusWriter)(nil)
var _ http.Hijacker = (*StatusWriter)(nil)


type Middleware func(http.Handler) http.Handler


func Chain(next http.Handler, mws ...Middleware) http.Handler {
	for i := len(mws) - 1; i >= 0; i-- {
		next = mws[i](next)
	}
	return next
}

func clientIP(r *http.Request) string {
	ipStr, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		ipStr = r.RemoteAddr
	}
	ip, err := netip.ParseAddr(ipStr)
	if err != nil {
		return ipStr
	}
	ip = ip.Unmap()
	if ip.Is4() {
		return ip.String()
	}
	prefix := netip.PrefixFrom(ip, 64)
	return prefix.Masked().String()
}

type StatusWriter struct {
	http.ResponseWriter
	status      int
	bytes       int
	wroteHeader bool
}

func (w *StatusWriter) WriteHeader(code int) {
	if !w.wroteHeader {
		w.status = code
		w.wroteHeader = true
	}
	w.ResponseWriter.WriteHeader(code)
}

func (w *StatusWriter) Write(b []byte) (int, error) {
	n, err := w.ResponseWriter.Write(b)
	w.bytes += n
	w.wroteHeader = true
	return n, err
}

func (w *StatusWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	hijacker, ok := w.ResponseWriter.(http.Hijacker)
	if !ok {
		return nil, nil, http.ErrNotSupported
	}

	conn, rw, err := hijacker.Hijack()
	if err == nil {
		if !w.wroteHeader {
			w.status = 101
			w.wroteHeader = true
		}
	}
	return conn, rw, err
}

func (w *StatusWriter) Flush() {
	f, ok := w.ResponseWriter.(http.Flusher)
	if !ok {
		return
	}
	f.Flush()
}

func (w *StatusWriter) Unwrap() http.ResponseWriter {
	return w.ResponseWriter
}
