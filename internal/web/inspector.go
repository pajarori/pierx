package web

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"sync"
	"sync/atomic"
	"time"
)

const (
	maxCapturedBody      = 1 << 20
	maxRequestsPerTunnel = 100
)

type CapturedRequest struct {
	ID              string        `json:"id"`
	Time            time.Time     `json:"time"`
	Method          string        `json:"method"`
	URL             string        `json:"url"`
	RequestHeaders  http.Header   `json:"request_headers"`
	RequestBody     string        `json:"request_body,omitempty"`
	StatusCode      int           `json:"status_code"`
	ResponseHeaders http.Header   `json:"response_headers,omitempty"`
	ResponseBody    string        `json:"response_body,omitempty"`
	Duration        time.Duration `json:"duration_ms"`
	Subdomain       string        `json:"subdomain"`
	Mode            string        `json:"mode"`
}

func (c CapturedRequest) MarshalJSON() ([]byte, error) {
	type capturedRequestJSON struct {
		ID              string      `json:"id"`
		Time            time.Time   `json:"time"`
		Method          string      `json:"method"`
		URL             string      `json:"url"`
		RequestHeaders  http.Header `json:"request_headers"`
		RequestBody     string      `json:"request_body,omitempty"`
		StatusCode      int         `json:"status_code"`
		ResponseHeaders http.Header `json:"response_headers,omitempty"`
		ResponseBody    string      `json:"response_body,omitempty"`
		DurationMS      int64       `json:"duration_ms"`
		Subdomain       string      `json:"subdomain"`
		Mode            string      `json:"mode"`
	}
	return json.Marshal(capturedRequestJSON{
		ID:              c.ID,
		Time:            c.Time,
		Method:          c.Method,
		URL:             c.URL,
		RequestHeaders:  c.RequestHeaders,
		RequestBody:     c.RequestBody,
		StatusCode:      c.StatusCode,
		ResponseHeaders: c.ResponseHeaders,
		ResponseBody:    c.ResponseBody,
		DurationMS:      c.Duration.Milliseconds(),
		Subdomain:       c.Subdomain,
		Mode:            c.Mode,
	})
}

type Inspector struct {
	mu       sync.RWMutex
	requests map[string][]*CapturedRequest
	subs     map[string][]chan *CapturedRequest
	counter  atomic.Int64
}

func NewInspector() *Inspector {
	return &Inspector{
		requests: make(map[string][]*CapturedRequest),
		subs:     make(map[string][]chan *CapturedRequest),
	}
}

func (ins *Inspector) Capture(req *CapturedRequest) {
	ins.mu.Lock()
	defer ins.mu.Unlock()

	reqs := ins.requests[req.Subdomain]
	reqs = append(reqs, req)
	if len(reqs) > maxRequestsPerTunnel {
		reqs = reqs[len(reqs)-maxRequestsPerTunnel:]
	}
	ins.requests[req.Subdomain] = reqs

	for _, ch := range ins.subs[req.Subdomain] {
		select {
		case ch <- req:
		default:
		}
	}
	for _, ch := range ins.subs[""] {
		select {
		case ch <- req:
		default:
		}
	}
}

func (ins *Inspector) GetRequests(subdomain string) []*CapturedRequest {
	ins.mu.RLock()
	defer ins.mu.RUnlock()
	return ins.requests[subdomain]
}

func (ins *Inspector) ClearRequests(subdomain string) {
	ins.mu.Lock()
	defer ins.mu.Unlock()
	delete(ins.requests, subdomain)
}

func (ins *Inspector) Subscribe(subdomain string) chan *CapturedRequest {
	ch := make(chan *CapturedRequest, 32)
	ins.mu.Lock()
	ins.subs[subdomain] = append(ins.subs[subdomain], ch)
	ins.mu.Unlock()
	return ch
}

func (ins *Inspector) Unsubscribe(subdomain string, ch chan *CapturedRequest) {
	ins.mu.Lock()
	defer ins.mu.Unlock()
	subs := ins.subs[subdomain]
	for i, s := range subs {
		if s == ch {
			ins.subs[subdomain] = append(subs[:i], subs[i+1:]...)
			close(ch)
			return
		}
	}
}

func (ins *Inspector) NextID() string {
	id := ins.counter.Add(1)
	return time.Now().Format("150405") + "-" + itoa(id)
}

func itoa(n int64) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	pos := len(buf)
	for n > 0 {
		pos--
		buf[pos] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[pos:])
}

func CaptureRequestBody(r *http.Request) string {
	if r.Body == nil {
		return ""
	}
	body, _ := io.ReadAll(io.LimitReader(r.Body, maxCapturedBody))
	r.Body.Close()
	r.Body = io.NopCloser(bytes.NewReader(body))
	return string(body)
}
