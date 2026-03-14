package tunnel

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/hashicorp/yamux"
	"github.com/pajarori/pierx/internal/p2p"
	"github.com/rs/zerolog/log"
)

type Client struct {
	serverAddr string
	localPort  int
	pubAddr    string
	signalAddr string
	typeName   string
	tcpAllow   []string

	conn       net.Conn
	muxSession *yamux.Session
	muxMu      sync.Mutex
	ctx        context.Context
	cancel     context.CancelFunc
	wg         sync.WaitGroup

	Assigned   string
	SessionID  string
	PublicURL  string
	InspectURL string
	Mode       string
	Anonymous  bool
	OnRequest  func(RequestEvent)
	OnState    func(StateEvent)
}

type RequestEvent struct {
	StreamID        int
	Time            time.Time
	Method          string
	URL             string
	RequestHeaders  http.Header
	RequestBody     string
	StatusCode      int
	ResponseHeaders http.Header
	ResponseBody    string
	Duration        time.Duration
}

type StateEvent struct {
	Status  string
	Err     error
	Latency string
}

func NewClient(serverAddr string, localPort int, pubAddr, signalAddr, typeName string, tcpAllow []string) *Client {
	ctx, cancel := context.WithCancel(context.Background())
	if typeName == "" {
		typeName = "http"
	}
	return &Client{
		serverAddr: serverAddr,
		localPort:  localPort,
		pubAddr:    pubAddr,
		signalAddr: signalAddr,
		typeName:   typeName,
		tcpAllow:   append([]string(nil), tcpAllow...),
		ctx:        ctx,
		cancel:     cancel,
	}
}

func (c *Client) Connect() error {
	_, err := c.connectOnce()
	return err
}

func (c *Client) ConnectWithRetry(ctx context.Context) error {
	backoff := time.Second
	maxBackoff := 30 * time.Second

	for {
		_, err := c.connectOnce()
		if err == nil {
			return nil
		}

		log.Warn().Err(err).Dur("retry_in", backoff).Msg("connection failed, retrying")

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(backoff):
		}

		backoff *= 2
		if backoff > maxBackoff {
			backoff = maxBackoff
		}
	}
}

func (c *Client) RunWithRetry(ctx context.Context, onConnect func(*RegistrationResp)) error {
	backoff := time.Second
	maxBackoff := 30 * time.Second

	for {
		c.emitState("connecting", nil)
		resp, err := c.connectOnce()
		if err != nil {
			c.emitState("reconnecting", err)
			log.Warn().Err(err).Dur("retry_in", backoff).Msg("connection failed, retrying")
			select {
			case <-ctx.Done():
				return nil
			case <-time.After(backoff):
			}
			backoff *= 2
			if backoff > maxBackoff {
				backoff = maxBackoff
			}
			continue
		}

		backoff = time.Second
		c.emitState("connected", nil)
		if onConnect != nil {
			onConnect(resp)
		}
		go c.heartbeat(ctx)
		go c.runP2P(ctx)

		err = c.Serve(ctx)
		c.closeActive()
		if ctx.Err() != nil {
			c.emitState("disconnected", nil)
			return nil
		}
		c.emitState("reconnecting", err)
		log.Warn().Err(err).Dur("retry_in", backoff).Msg("tunnel disconnected, reconnecting")
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(backoff):
		}
	}
}

func (c *Client) emitState(status string, err error) {
	if c.OnState != nil {
		c.OnState(StateEvent{Status: status, Err: err})
	}
}

func (c *Client) heartbeat(ctx context.Context) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			c.muxMu.Lock()
			mux := c.muxSession
			c.muxMu.Unlock()
			if mux == nil {
				return
			}
			start := time.Now()
			stream, err := mux.Open()
			if err != nil {
				c.emitState("reconnecting", err)
				return
			}
			_ = json.NewEncoder(stream).Encode(ControlMsg{Type: "ping"})
			var resp ControlResp
			err = json.NewDecoder(stream).Decode(&resp)
			_ = stream.Close()
			if err != nil || !resp.OK {
				c.emitState("reconnecting", err)
				return
			}
			if c.OnState != nil {
				c.OnState(StateEvent{Status: "connected", Latency: formatLatency(time.Since(start))})
			}
		}
	}
}

func (c *Client) connectOnce() (*RegistrationResp, error) {
	c.closeActive()
	conn, err := net.DialTimeout("tcp", c.serverAddr, 10*time.Second)
	if err != nil {
		return nil, fmt.Errorf("dial server: %w", err)
	}
	c.conn = conn

	reg := RegistrationMsg{
		LocalPort: c.localPort,
		PubAddr:   c.pubAddr,
		Type:      c.tunnelType(),
		TCPAllow:  append([]string(nil), c.tcpAllow...),
	}
	data, _ := json.Marshal(reg)
	if _, err := conn.Write(data); err != nil {
		conn.Close()
		return nil, fmt.Errorf("send registration: %w", err)
	}

	buf := make([]byte, 4096)
	conn.SetReadDeadline(time.Now().Add(10 * time.Second))
	n, err := conn.Read(buf)
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("read registration response: %w", err)
	}
	conn.SetReadDeadline(time.Time{})

	var resp RegistrationResp
	if err := json.Unmarshal(buf[:n], &resp); err != nil {
		conn.Close()
		return nil, fmt.Errorf("parse registration response: %w", err)
	}
	if !resp.OK {
		conn.Close()
		return nil, fmt.Errorf("registration rejected: %s", resp.Error)
	}

	c.Assigned = resp.Subdomain
	c.SessionID = resp.SessionID
	c.PublicURL = resp.PublicURL
	c.InspectURL = resp.InspectURL
	c.Mode = resp.Mode
	c.Anonymous = resp.Anonymous

	muxCfg := yamux.DefaultConfig()
	muxCfg.KeepAliveInterval = 15 * time.Second
	muxCfg.ConnectionWriteTimeout = 10 * time.Second

	muxSession, err := yamux.Client(conn, muxCfg)
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("yamux client setup: %w", err)
	}
	c.muxMu.Lock()
	c.muxSession = muxSession
	c.muxMu.Unlock()

	log.Info().
		Str("subdomain", c.Assigned).
		Str("session_id", c.SessionID).
		Bool("anonymous", c.Anonymous).
		Str("server", c.serverAddr).
		Msg("tunnel connected")

	return &resp, nil
}

func (c *Client) Serve(ctx context.Context) error {
	streamCount := 0
	for {
		stream, err := c.muxSession.Accept()
		if err != nil {
			select {
			case <-ctx.Done():
				return nil
			default:
				return fmt.Errorf("accept stream: %w", err)
			}
		}

		streamCount++
		id := streamCount
		c.wg.Add(1)
		go func() {
			defer c.wg.Done()
			if c.tunnelType() == "tcp" {
				c.handleTCPStream(id, stream)
				return
			}
			c.handleStream(id, stream)
		}()
	}
}

func (c *Client) tunnelType() string {
	return c.typeName
}

func (c *Client) handleStream(id int, stream net.Conn) {
	defer stream.Close()
	start := time.Now()
	reader := bufio.NewReader(stream)
	req, reqBody, err := readHTTPRequest(reader)
	if err != nil {
		c.proxyRawStream(id, stream, reader)
		return
	}
	if isUpgradeRequest(req) {
		c.proxyHTTPUpgrade(id, stream, req, reqBody)
		return
	}

	local, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", c.localPort), 5*time.Second)
	if err != nil {
		log.Error().Err(err).Int("stream", id).Int("port", c.localPort).Msg("failed to connect to local service")
		c.emitRequest(RequestEvent{
			StreamID:       id,
			Time:           start,
			Method:         req.Method,
			URL:            req.URL.String(),
			RequestHeaders: req.Header.Clone(),
			RequestBody:    reqBody,
		})
		return
	}
	defer local.Close()

	if err := req.Write(local); err != nil {
		return
	}

	resp, respBody, err := readHTTPResponse(local, req)
	if err != nil {
		return
	}
	defer resp.Body.Close()

	if err := resp.Write(stream); err != nil {
		return
	}

	c.emitRequest(RequestEvent{
		StreamID:        id,
		Time:            start,
		Method:          req.Method,
		URL:             req.URL.String(),
		RequestHeaders:  req.Header.Clone(),
		RequestBody:     reqBody,
		StatusCode:      resp.StatusCode,
		ResponseHeaders: resp.Header.Clone(),
		ResponseBody:    respBody,
		Duration:        time.Since(start),
	})
}

func readHTTPRequest(r *bufio.Reader) (*http.Request, string, error) {
	req, err := http.ReadRequest(r)
	if err != nil {
		return nil, "", err
	}
	body, _ := io.ReadAll(io.LimitReader(req.Body, 1<<20))
	_ = req.Body.Close()
	req.Body = io.NopCloser(bytes.NewReader(body))
	if req.URL != nil && req.URL.Scheme == "" {
		scheme := "http"
		if req.TLS != nil {
			scheme = "https"
		}
		req.URL.Scheme = scheme
		if req.Host != "" {
			req.URL.Host = req.Host
		}
	}
	return req, string(body), nil
}

func readHTTPResponse(conn net.Conn, req *http.Request) (*http.Response, string, error) {
	resp, err := http.ReadResponse(bufio.NewReader(conn), req)
	if err != nil {
		return nil, "", err
	}
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	_ = resp.Body.Close()
	resp.Body = io.NopCloser(bytes.NewReader(body))
	return resp, string(body), nil
}

func isUpgradeRequest(req *http.Request) bool {
	if !strings.EqualFold(req.Header.Get("Upgrade"), "websocket") {
		return false
	}
	for _, part := range strings.Split(req.Header.Get("Connection"), ",") {
		if strings.EqualFold(strings.TrimSpace(part), "upgrade") {
			return true
		}
	}
	return false
}

func (c *Client) proxyHTTPUpgrade(id int, stream net.Conn, req *http.Request, reqBody string) {
	start := time.Now()
	local, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", c.localPort), 5*time.Second)
	if err != nil {
		return
	}
	defer local.Close()
	if err := req.Write(local); err != nil {
		return
	}
	resp, err := http.ReadResponse(bufio.NewReader(local), req)
	if err != nil {
		return
	}
	defer resp.Body.Close()
	if err := resp.Write(stream); err != nil {
		return
	}
	c.emitRequest(RequestEvent{
		StreamID:        id,
		Time:            start,
		Method:          req.Method,
		URL:             req.URL.String(),
		RequestHeaders:  req.Header.Clone(),
		RequestBody:     reqBody,
		StatusCode:      resp.StatusCode,
		ResponseHeaders: resp.Header.Clone(),
		Duration:        time.Since(start),
	})
	var wg sync.WaitGroup
	wg.Add(2)
	cp := func(dst io.WriteCloser, src io.Reader) {
		defer wg.Done()
		_, _ = io.Copy(dst, src)
		_ = dst.Close()
	}
	go cp(local, stream)
	go cp(stream, local)
	wg.Wait()
}

func (c *Client) proxyRawStream(id int, stream net.Conn, reader *bufio.Reader) {
	local, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", c.localPort), 5*time.Second)
	if err != nil {
		return
	}
	defer local.Close()
	var wg sync.WaitGroup
	wg.Add(2)
	cp := func(dst io.WriteCloser, src io.Reader) {
		defer wg.Done()
		_, _ = io.Copy(dst, src)
		_ = dst.Close()
	}
	go cp(local, reader)
	go cp(stream, local)
	wg.Wait()
}

func (c *Client) handleTCPStream(id int, stream net.Conn) {
	defer stream.Close()
	start := time.Now()
	local, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", c.localPort), 5*time.Second)
	if err != nil {
		c.emitRequest(RequestEvent{
			StreamID: id,
			Time:     start,
			Method:   "TCP",
			URL:      stream.RemoteAddr().String(),
			Duration: time.Since(start),
		})
		return
	}
	defer local.Close()
	pipe(stream, local)
	c.emitRequest(RequestEvent{
		StreamID:   id,
		Time:       start,
		Method:     "TCP",
		URL:        stream.RemoteAddr().String(),
		StatusCode: 200,
		Duration:   time.Since(start),
	})
}

func (c *Client) emitRequest(ev RequestEvent) {
	if c.OnRequest != nil {
		c.OnRequest(ev)
	}
}

func formatLatency(d time.Duration) string {
	if d < time.Millisecond {
		return fmt.Sprintf("%dµs", d.Microseconds())
	}
	if d < time.Second {
		return fmt.Sprintf("%dms", d.Milliseconds())
	}
	return fmt.Sprintf("%.1fs", d.Seconds())
}

func (c *Client) runP2P(ctx context.Context) {
	if c.pubAddr == "" || c.signalAddr == "" {
		return
	}

	sc := p2p.NewSignalClient(c.signalAddr, c.Assigned, c.pubAddr)
	defer sc.Close()

	go sc.Run(ctx)

	for {
		select {
		case <-ctx.Done():
			return
		case req := <-sc.PunchRequests():
			log.Info().Str("remote", req.PubAddr).Str("session", req.SessionID).Msg("attempting UDP hole-punch")
			result, err := p2p.Punch(ctx, 0, req.PubAddr, req.SessionID, 10*time.Second)
			if err != nil {
				log.Warn().Err(err).Msg("punch error")
				continue
			}
			if result.Success {
				c.emitState("p2p", nil)
				log.Info().
					Str("local", result.LocalAddr.String()).
					Str("remote", result.RemoteAddr.String()).
					Msg("P2P hole-punch succeeded, direct connection available")
				result.Conn.Close()
			} else {
				log.Info().Msg("punch failed, continuing in relay mode")
			}
		}
	}
}

func (c *Client) Close() {
	c.cancel()
	c.closeActive()
	c.wg.Wait()
}

func (c *Client) closeActive() {
	c.muxMu.Lock()
	mux := c.muxSession
	c.muxSession = nil
	conn := c.conn
	c.conn = nil
	c.muxMu.Unlock()
	if mux != nil {
		mux.Close()
	}
	if conn != nil {
		conn.Close()
	}
	c.wg.Wait()
}
