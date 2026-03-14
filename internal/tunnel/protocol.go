package tunnel

import (
    "fmt"
    "io"
    "net"
    "sync"
)

type RegistrationMsg struct {
    LocalPort int      `json:"local_port"`
    PubAddr   string   `json:"pub_addr"`
    Type      string   `json:"type"`
    TCPAllow  []string `json:"tcp_allow,omitempty"`
}

type RegistrationResp struct {
    OK         bool   `json:"ok"`
    Subdomain  string `json:"subdomain"`
    SessionID  string `json:"session_id,omitempty"`
    PublicURL  string `json:"public_url,omitempty"`
    InspectURL string `json:"inspect_url,omitempty"`
    Mode       string `json:"mode,omitempty"`
    Anonymous  bool   `json:"anonymous"`
    Error      string `json:"error,omitempty"`
}

type ControlMsg struct {
    Type string `json:"type"`
}

type ControlResp struct {
    OK bool `json:"ok"`
}

func pipe(a, b io.ReadWriteCloser) {
    var wg sync.WaitGroup
    wg.Add(2)
    cp := func(dst, src io.ReadWriteCloser) {
        defer wg.Done()
        _, _ = io.Copy(dst, src)
        _ = dst.Close()
    }
    go cp(a, b)
    go cp(b, a)
    wg.Wait()
}

func min(a, b int) int {
    if a < b {
        return a
    }
    return b
}

func openError(msg string, err error) error {
    if err == nil {
        return nil
    }
    return fmt.Errorf("%s: %w", msg, err)
}

var _ net.Conn
