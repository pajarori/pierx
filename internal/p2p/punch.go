package p2p

import (
	"context"
	"fmt"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog/log"
)

const (
	punchPrefix    = "PUNCH\x00"
	punchAckPrefix = "PUNCH_ACK\x00"
	maxPunchPacket = 256
)

type PunchResult struct {
	Success    bool
	LocalAddr  *net.UDPAddr
	RemoteAddr *net.UDPAddr
	Conn       *net.UDPConn
}

func Punch(ctx context.Context, localPort int, remoteAddr string, sessionID string, timeout time.Duration) (*PunchResult, error) {
	remote, err := net.ResolveUDPAddr("udp4", remoteAddr)
	if err != nil {
		return nil, fmt.Errorf("resolve remote: %w", err)
	}

	local := &net.UDPAddr{Port: localPort}
	conn, err := net.ListenUDP("udp4", local)
	if err != nil {
		return nil, fmt.Errorf("listen udp: %w", err)
	}

	result := &PunchResult{
		LocalAddr:  conn.LocalAddr().(*net.UDPAddr),
		RemoteAddr: remote,
		Conn:       conn,
	}

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	var (
		gotPunch    bool
		gotAck      bool
		mu          sync.Mutex
		confirmedCh = make(chan struct{}, 1)
	)

	go func() {
		payload := []byte(punchPrefix + sessionID)
		ticker := time.NewTicker(200 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if _, err := conn.WriteToUDP(payload, remote); err != nil {
					log.Debug().Err(err).Msg("punch send error")
				}
			}
		}
	}()

	go func() {
		buf := make([]byte, maxPunchPacket)
		for {
			conn.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
			n, addr, err := conn.ReadFromUDP(buf)
			if err != nil {
				select {
				case <-ctx.Done():
					return
				default:
					continue
				}
			}

			data := string(buf[:n])

			mu.Lock()
			if strings.HasPrefix(data, punchPrefix) && !gotPunch {
				gotPunch = true
				log.Debug().Str("from", addr.String()).Msg("received PUNCH")
				ackPayload := []byte(punchAckPrefix + sessionID)
				conn.WriteToUDP(ackPayload, addr)
				result.RemoteAddr = addr
				if gotAck {
					select {
					case confirmedCh <- struct{}{}:
					default:
					}
				}
			} else if strings.HasPrefix(data, punchAckPrefix) && !gotAck {
				gotAck = true
				log.Debug().Str("from", addr.String()).Msg("received PUNCH_ACK")
				result.RemoteAddr = addr
				if gotPunch {
					select {
					case confirmedCh <- struct{}{}:
					default:
					}
				}
			}
			mu.Unlock()
		}
	}()

	select {
	case <-confirmedCh:
		result.Success = true
		conn.SetReadDeadline(time.Time{})
		log.Info().
			Str("local", result.LocalAddr.String()).
			Str("remote", result.RemoteAddr.String()).
			Msg("UDP hole-punch succeeded")
		return result, nil
	case <-ctx.Done():
		conn.Close()
		log.Info().Str("remote", remoteAddr).Msg("UDP hole-punch failed (timeout)")
		return result, nil
	}
}
