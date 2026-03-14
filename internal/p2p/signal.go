package p2p

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/rs/zerolog/log"
)

type MsgType string

const (
	MsgRegister   MsgType = "register"
	MsgRegistered MsgType = "registered"
	MsgPunchStart MsgType = "punch_start"
	MsgPunchFail  MsgType = "punch_fail"
	MsgError      MsgType = "error"
)

type SignalMsg struct {
	Type      MsgType         `json:"type"`
	From      string          `json:"from,omitempty"`
	To        string          `json:"to,omitempty"`
	Subdomain string          `json:"subdomain,omitempty"`
	Payload   json.RawMessage `json:"payload,omitempty"`
}

type PunchStartPayload struct {
	PubAddr   string `json:"pub_addr"`
	SessionID string `json:"session_id"`
}

type SignalClient struct {
	url       string
	subdomain string
	pubAddr   string

	mu      sync.Mutex
	conn    *websocket.Conn
	punchCh chan PunchStartPayload
}

func NewSignalClient(signalURL, subdomain, pubAddr string) *SignalClient {
	return &SignalClient{
		url:       signalURL,
		subdomain: subdomain,
		pubAddr:   pubAddr,
		punchCh:   make(chan PunchStartPayload, 8),
	}
}

func (sc *SignalClient) PunchRequests() <-chan PunchStartPayload {
	return sc.punchCh
}

func (sc *SignalClient) Run(ctx context.Context) {
	backoff := time.Second
	maxBackoff := 30 * time.Second

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		err := sc.connectAndListen(ctx)
		if ctx.Err() != nil {
			return
		}
		if err != nil {
			log.Warn().Err(err).Dur("retry_in", backoff).Msg("signal connection failed, retrying")
		}

		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}

		backoff *= 2
		if backoff > maxBackoff {
			backoff = maxBackoff
		}
	}
}

func (sc *SignalClient) connectAndListen(ctx context.Context) error {
	dialer := websocket.Dialer{
		HandshakeTimeout: 10 * time.Second,
	}

	conn, _, err := dialer.DialContext(ctx, sc.url+"/signal", nil)
	if err != nil {
		return err
	}

	sc.mu.Lock()
	sc.conn = conn
	sc.mu.Unlock()

	defer func() {
		conn.Close()
		sc.mu.Lock()
		sc.conn = nil
		sc.mu.Unlock()
	}()

	regPayload, _ := json.Marshal(struct {
		PubAddr string `json:"pub_addr"`
	}{PubAddr: sc.pubAddr})

	regMsg := SignalMsg{
		Type:      MsgRegister,
		Subdomain: sc.subdomain,
		Payload:   regPayload,
	}
	data, _ := json.Marshal(regMsg)
	if err := conn.WriteMessage(websocket.TextMessage, data); err != nil {
		return err
	}

	log.Debug().Str("subdomain", sc.subdomain).Str("url", sc.url).Msg("signal: registering")

	for {
		select {
		case <-ctx.Done():
			return nil
		default:
		}

		_, msgData, err := conn.ReadMessage()
		if err != nil {
			return err
		}

		var msg SignalMsg
		if err := json.Unmarshal(msgData, &msg); err != nil {
			continue
		}

		switch msg.Type {
		case MsgRegistered:
			log.Info().Str("subdomain", sc.subdomain).Msg("signal: registered")
		case MsgPunchStart:
			var payload PunchStartPayload
			if err := json.Unmarshal(msg.Payload, &payload); err != nil {
				log.Warn().Err(err).Msg("signal: invalid punch_start payload")
				continue
			}
			log.Info().Str("pub_addr", payload.PubAddr).Msg("signal: received punch_start")
			select {
			case sc.punchCh <- payload:
			default:
				log.Warn().Msg("signal: punch channel full, dropping")
			}
		case MsgError:
			log.Warn().RawJSON("payload", msg.Payload).Msg("signal: server error")
		}
	}
}

func (sc *SignalClient) Close() {
	sc.mu.Lock()
	defer sc.mu.Unlock()
	if sc.conn != nil {
		sc.conn.Close()
		sc.conn = nil
	}
}
