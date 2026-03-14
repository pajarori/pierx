package cmd

import (
	"context"
	"fmt"
	"io"
	"net"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/spf13/cobra"

	"github.com/pajarori/pierx/internal/p2p"
	"github.com/pajarori/pierx/internal/tui"
	"github.com/pajarori/pierx/internal/tunnel"
	"github.com/pajarori/pierx/internal/web"
	"github.com/pajarori/pierx/pkg/config"
)

var version = "0.1.2"
var silent bool

func Execute() error {
	log.Logger = log.Output(zerolog.ConsoleWriter{Out: os.Stderr})

	rootCmd := &cobra.Command{
		Use:   "pierx",
		Short: "pierx tunnel client",
		Long:  "Connect to a pierx server and expose a local service",
		Example: "  pierx http 3000\n" +
			"  pierx tcp 22",
	}
	rootCmd.PersistentFlags().BoolVar(&silent, "silent", false, "Run without any output")

	rootCmd.AddCommand(httpCmd())
	rootCmd.AddCommand(tcpCmd())
	rootCmd.AddCommand(statusCmd())

	return rootCmd.Execute()
}

func httpCmd() *cobra.Command {
	cmd := newClientCmd(
		"http [port]",
		"Expose a local HTTP service",
		"  pierx http 3000\n  pierx http 8080",
	)
	return cmd
}

func tcpCmd() *cobra.Command {
	return newClientCmd(
		"tcp [port]",
		"Expose a local TCP service",
		"  pierx tcp 22\n  pierx tcp 5432",
	)
}

func newClientCmd(use, short, example string) *cobra.Command {
	cfg := config.DefaultClientConfig()

	cmd := &cobra.Command{
		Use:     use,
		Short:   short,
		Args:    cobra.MaximumNArgs(1),
		Example: example,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg.TunnelType = strings.Fields(use)[0]
			if err := applyClientArgs(cfg, args); err != nil {
				return err
			}
			if silent {
				return runClientSilent(cfg)
			}
			return runClientTUI(cfg)
		},
	}

	f := cmd.Flags()
	f.IntVar(&cfg.LocalPort, "port", cfg.LocalPort, "Local port to expose")
	f.StringSliceVar(&cfg.TCPAllow, "allow", cfg.TCPAllow, "Allowed source IPs/CIDRs for tcp tunnels")

	return cmd
}

func statusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show current tunnel status",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Println("No active tunnel. Use 'pierx http 3000' or 'pierx tcp 22' to start one.")
		},
	}
}

func setupClient(cfg *config.ClientConfig) (*tunnel.Client, *web.Inspector, string, error) {
	if err := config.ValidateClientConfig(cfg); err != nil {
		return nil, nil, "", err
	}

	var pubAddr string
	if cfg.STUNAddr != "" {
		addr, err := p2p.DiscoverPublicAddr(cfg.STUNAddr)
		if err != nil {
			log.Warn().Err(err).Msg("STUN discovery failed (P2P will be unavailable)")
		} else {
			pubAddr = addr
		}
	}

	client := tunnel.NewClient(cfg.ServerAddr, cfg.LocalPort, pubAddr, cfg.SignalAddr, cfg.TunnelType, cfg.TCPAllow)
	ins := web.NewInspector()
	client.OnRequest = func(ev tunnel.RequestEvent) {
		mode := "relay"
		if cfg.TunnelType == "tcp" {
			mode = "tcp"
		}
		ins.Capture(&web.CapturedRequest{
			ID:              fmt.Sprintf("local-%d", ev.StreamID),
			Time:            ev.Time,
			Method:          ev.Method,
			URL:             ev.URL,
			RequestHeaders:  ev.RequestHeaders,
			RequestBody:     ev.RequestBody,
			StatusCode:      ev.StatusCode,
			ResponseHeaders: ev.ResponseHeaders,
			ResponseBody:    ev.ResponseBody,
			Duration:        ev.Duration,
			Subdomain:       client.Assigned,
			Mode:            mode,
		})
	}

	return client, ins, pubAddr, nil
}

func buildTunnelURL(cfg *config.ClientConfig, subdomain string) string {
	serverHost := serverHost(cfg.ServerAddr)
	return fmt.Sprintf("https://%s.%s", subdomain, serverHost)
}
func serverHost(addr string) string {
	host, _, err := net.SplitHostPort(addr)
	if err == nil {
		return host
	}
	if idx := strings.LastIndex(addr, ":"); idx != -1 {
		return addr[:idx]
	}
	return addr
}

func applyClientArgs(cfg *config.ClientConfig, args []string) error {
	if len(args) == 0 {
		return nil
	}
	port, err := strconv.Atoi(args[0])
	if err != nil {
		return fmt.Errorf("invalid port %q", args[0])
	}
	cfg.LocalPort = port
	return nil
}

func runClientTUI(cfg *config.ClientConfig) error {
	client, ins, pubAddr, err := setupClient(cfg)
	if err != nil {
		return err
	}

	restoreLogs := silenceLogs()
	defer restoreLogs()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	reqCh := ins.Subscribe("")
	info := tui.ConnInfo{
		Version:   version,
		TunnelURL: "connecting...",
		Mode:      strings.ToUpper(cfg.TunnelType) + " connecting",
		Auth:      "anonymous",
		LocalAddr: localAddr(cfg),
		PubAddr:   pubAddr,
		Status:    "connecting",
		Latency:   "--",
	}

	errCh := make(chan error, 1)

	p := tea.NewProgram(
		tui.New(info, reqCh),
		tea.WithAltScreen(),
	)

	client.OnState = func(ev tunnel.StateEvent) {
		latency := ev.Latency
		if latency == "" && ev.Err != nil && ev.Status == "reconnecting" {
			latency = "offline"
		}
		p.Send(tui.StatusMsg{Status: ev.Status, Latency: latency})
	}

	go func() {
		err := client.RunWithRetry(ctx, func(resp *tunnel.RegistrationResp) {
			p.Send(tui.SessionMsg{Info: connInfo(cfg, client, pubAddr)})
		})
		errCh <- err
	}()

	go func() {
		select {
		case err := <-errCh:
			if err != nil {
				p.Send(tui.ErrorMsg{Err: err})
			}
			p.Send(tea.Quit())
		case <-ctx.Done():
		}
	}()

	if _, err := p.Run(); err != nil {
		return err
	}

	cancel()
	return nil
}

func runClientSilent(cfg *config.ClientConfig) error {
	client, _, _, err := setupClient(cfg)
	if err != nil {
		return err
	}
	defer client.Close()

	restoreLogs := silenceLogs()
	defer restoreLogs()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	return client.RunWithRetry(ctx, nil)
}

func silenceLogs() func() {
	prev := log.Logger
	level := zerolog.GlobalLevel()
	zerolog.SetGlobalLevel(zerolog.ErrorLevel)
	log.Logger = zerolog.New(io.Discard).With().Timestamp().Logger()
	return func() {
		log.Logger = prev
		zerolog.SetGlobalLevel(level)
	}
}

func connInfo(cfg *config.ClientConfig, client *tunnel.Client, pubAddr string) tui.ConnInfo {
	mode := strings.ToUpper(cfg.TunnelType) + " relay"
	if cfg.TunnelType == "http" {
		mode = "HTTP relay (P2P-ready)"
	}
	tunnelURL := client.PublicURL
	if tunnelURL == "" {
		if cfg.TunnelType == "tcp" {
			tunnelURL = fmt.Sprintf("tcp://%s", cfg.ServerAddr)
		} else {
			tunnelURL = buildTunnelURL(cfg, client.Assigned)
		}
	}
	return tui.ConnInfo{
		Version:   version,
		TunnelURL: tunnelURL,
		Mode:      mode,
		Auth:      "anonymous",
		LocalAddr: localAddr(cfg),
		PubAddr:   pubAddr,
		Status:    "connected",
		Latency:   "--",
	}
}

func localAddr(cfg *config.ClientConfig) string {
	if cfg.TunnelType == "tcp" {
		return fmt.Sprintf("tcp://localhost:%d", cfg.LocalPort)
	}
	return fmt.Sprintf("http://localhost:%d", cfg.LocalPort)
}
