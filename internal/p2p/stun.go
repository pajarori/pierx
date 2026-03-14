package p2p

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"fmt"
	"net"
	"time"

	"github.com/rs/zerolog/log"
)

const (
	stunMagicCookie     = 0x2112A442
	stunBindingRequest  = 0x0001
	stunBindingResponse = 0x0101
	stunHeaderSize      = 20
	attrXORMappedAddr   = 0x0020
)

type STUNServer struct {
	addr string
	conn *net.UDPConn
}

func NewSTUNServer(addr string) *STUNServer {
	return &STUNServer{addr: addr}
}

func (s *STUNServer) ListenAndServe(ctx context.Context) error {
	udpAddr, err := net.ResolveUDPAddr("udp4", s.addr)
	if err != nil {
		return fmt.Errorf("resolve stun addr: %w", err)
	}
	conn, err := net.ListenUDP("udp4", udpAddr)
	if err != nil {
		return fmt.Errorf("listen stun: %w", err)
	}
	s.conn = conn
	log.Info().Str("addr", s.addr).Msg("STUN server listening")

	go func() {
		<-ctx.Done()
		conn.Close()
	}()

	buf := make([]byte, 1024)
	for {
		n, remoteAddr, err := conn.ReadFromUDP(buf)
		if err != nil {
			select {
			case <-ctx.Done():
				return nil
			default:
				log.Error().Err(err).Msg("stun read error")
				continue
			}
		}
		if n < stunHeaderSize {
			continue
		}
		pkt := make([]byte, n)
		copy(pkt, buf[:n])
		go s.handlePacket(pkt, remoteAddr)
	}
}

func (s *STUNServer) handlePacket(data []byte, remote *net.UDPAddr) {
	msgType := binary.BigEndian.Uint16(data[0:2])
	if msgType != stunBindingRequest {
		return
	}
	cookie := binary.BigEndian.Uint32(data[4:8])
	if cookie != stunMagicCookie {
		return
	}

	txnID := data[8:20]
	resp := buildBindingResponse(txnID, remote)

	if _, err := s.conn.WriteToUDP(resp, remote); err != nil {
		log.Error().Err(err).Msg("stun write error")
	}
	log.Debug().Str("remote", remote.String()).Msg("stun binding response sent")
}

func buildBindingResponse(txnID []byte, addr *net.UDPAddr) []byte {
	ip := addr.IP.To4()
	if ip == nil {
		ip = addr.IP.To16()
	}

	attrValue := make([]byte, 8)
	attrValue[0] = 0x00
	attrValue[1] = 0x01

	xorPort := uint16(addr.Port) ^ uint16(stunMagicCookie>>16)
	binary.BigEndian.PutUint16(attrValue[2:4], xorPort)

	magicBytes := make([]byte, 4)
	binary.BigEndian.PutUint32(magicBytes, stunMagicCookie)
	for i := 0; i < 4; i++ {
		attrValue[4+i] = ip[i] ^ magicBytes[i]
	}

	attr := make([]byte, 4+len(attrValue))
	binary.BigEndian.PutUint16(attr[0:2], attrXORMappedAddr)
	binary.BigEndian.PutUint16(attr[2:4], uint16(len(attrValue)))
	copy(attr[4:], attrValue)

	resp := make([]byte, stunHeaderSize+len(attr))
	binary.BigEndian.PutUint16(resp[0:2], stunBindingResponse)
	binary.BigEndian.PutUint16(resp[2:4], uint16(len(attr)))
	binary.BigEndian.PutUint32(resp[4:8], stunMagicCookie)
	copy(resp[8:20], txnID)
	copy(resp[20:], attr)

	return resp
}

func DiscoverPublicAddr(stunServer string) (string, error) {
	raddr, err := net.ResolveUDPAddr("udp4", stunServer)
	if err != nil {
		return "", fmt.Errorf("resolve stun server: %w", err)
	}
	conn, err := net.DialUDP("udp4", nil, raddr)
	if err != nil {
		return "", fmt.Errorf("dial stun server: %w", err)
	}
	defer conn.Close()

	req := buildBindingRequest()
	if _, err := conn.Write(req); err != nil {
		return "", fmt.Errorf("write stun request: %w", err)
	}

	buf := make([]byte, 1024)
	conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	n, err := conn.Read(buf)
	if err != nil {
		return "", fmt.Errorf("read stun response: %w", err)
	}
	if n < stunHeaderSize {
		return "", fmt.Errorf("stun response too short")
	}

	return parseBindingResponse(buf[:n])
}

func buildBindingRequest() []byte {
	pkt := make([]byte, stunHeaderSize)
	binary.BigEndian.PutUint16(pkt[0:2], stunBindingRequest)
	binary.BigEndian.PutUint16(pkt[2:4], 0)
	binary.BigEndian.PutUint32(pkt[4:8], stunMagicCookie)
	_, _ = rand.Read(pkt[8:20])
	return pkt
}

func parseBindingResponse(data []byte) (string, error) {
	msgType := binary.BigEndian.Uint16(data[0:2])
	if msgType != stunBindingResponse {
		return "", fmt.Errorf("not a binding response: 0x%04x", msgType)
	}
	msgLen := binary.BigEndian.Uint16(data[2:4])
	if int(msgLen)+stunHeaderSize > len(data) {
		return "", fmt.Errorf("truncated response")
	}

	offset := stunHeaderSize
	for offset < stunHeaderSize+int(msgLen) {
		if offset+4 > len(data) {
			break
		}
		attrType := binary.BigEndian.Uint16(data[offset : offset+2])
		attrLen := binary.BigEndian.Uint16(data[offset+2 : offset+4])
		if attrType == attrXORMappedAddr {
			if int(attrLen) < 8 {
				return "", fmt.Errorf("xor-mapped-address too short")
			}
			val := data[offset+4 : offset+4+int(attrLen)]
			xorPort := binary.BigEndian.Uint16(val[2:4])
			port := xorPort ^ uint16(stunMagicCookie>>16)

			magicBytes := make([]byte, 4)
			binary.BigEndian.PutUint32(magicBytes, stunMagicCookie)
			ip := make(net.IP, 4)
			for i := 0; i < 4; i++ {
				ip[i] = val[4+i] ^ magicBytes[i]
			}
			return fmt.Sprintf("%s:%d", ip.String(), port), nil
		}
		offset += 4 + int(attrLen)
		if offset%4 != 0 {
			offset += 4 - (offset % 4)
		}
	}
	return "", fmt.Errorf("no XOR-MAPPED-ADDRESS in response")
}
