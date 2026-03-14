package tui

import (
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/table"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/pajarori/pierx/internal/web"
)

type view int

const (
	viewList view = iota
	viewDetail
)

type ConnInfo struct {
	Version   string
	TunnelURL string
	Mode      string
	Auth      string
	LocalAddr string
	PubAddr   string
	Status    string
	Latency   string
}

type RequestMsg *web.CapturedRequest

type ErrorMsg struct{ Err error }

type SessionMsg struct{ Info ConnInfo }
type StatusMsg struct {
	Status  string
	Latency string
}

type Model struct {
	info     ConnInfo
	requests []*web.CapturedRequest
	reqCh    <-chan *web.CapturedRequest

	table  table.Model
	detail viewport.Model
	view   view
	width  int
	height int
	ready  bool
}

func New(info ConnInfo, reqCh <-chan *web.CapturedRequest) Model {
	columns := []table.Column{
		{Title: "Time", Width: 10},
		{Title: "Method", Width: 7},
		{Title: "Path", Width: 36},
		{Title: "Status", Width: 7},
		{Title: "Duration", Width: 10},
	}

	t := table.New(
		table.WithColumns(columns),
		table.WithFocused(true),
		table.WithHeight(10),
	)

	s := table.Styles{
		Header:   tableHeaderStyle,
		Cell:     tableCellStyle,
		Selected: tableSelectedStyle,
	}
	t.SetStyles(s)

	vp := viewport.New(80, 20)

	return Model{
		info:   info,
		reqCh:  reqCh,
		table:  t,
		detail: vp,
		view:   viewList,
	}
}

func (m Model) Init() tea.Cmd {
	return waitForRequest(m.reqCh)
}

func waitForRequest(ch <-chan *web.CapturedRequest) tea.Cmd {
	return func() tea.Msg {
		req, ok := <-ch
		if !ok {
			return nil
		}
		return RequestMsg(req)
	}
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch {
		case key.Matches(msg, keys.Quit):
			return m, tea.Quit

		case key.Matches(msg, keys.Back):
			if m.view == viewDetail {
				m.view = viewList
				m.table.Focus()
			}
			return m, nil

		case key.Matches(msg, keys.Enter):
			if m.view == viewList && len(m.requests) > 0 {
				m.view = viewDetail
				m.table.Blur()
				m.updateDetail()
			}
			return m, nil

		case key.Matches(msg, keys.Clear):
			if m.view == viewList {
				m.requests = nil
				m.table.SetRows(nil)
			}
			return m, nil
		}

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.ready = true
		headerH := lipgloss.Height(m.renderHeader()) + 1
		helpH := 2
		tableH := m.height - headerH - helpH - 2
		if tableH < 3 {
			tableH = 3
		}
		m.table.SetWidth(m.width)
		m.table.SetHeight(tableH)
		m.detail.Width = m.width - 4
		m.detail.Height = tableH
		m.resizeColumns()
		return m, nil

	case RequestMsg:
		if msg != nil {
			m.requests = append(m.requests, msg)
			m.rebuildRows()
			m.table.GotoBottom()
		}
		return m, waitForRequest(m.reqCh)

	case SessionMsg:
		m.info = msg.Info
		m.requests = nil
		m.table.SetRows(nil)
		m.resizeColumns()
		if m.view == viewDetail {
			m.view = viewList
			m.table.Focus()
		}
		return m, nil

	case StatusMsg:
		if msg.Status != "" {
			m.info.Status = msg.Status
		}
		if msg.Latency != "" {
			m.info.Latency = msg.Latency
		}
		return m, nil
	}

	if m.view == viewList {
		var cmd tea.Cmd
		m.table, cmd = m.table.Update(msg)
		cmds = append(cmds, cmd)
	} else {
		var cmd tea.Cmd
		m.detail, cmd = m.detail.Update(msg)
		cmds = append(cmds, cmd)
	}

	return m, tea.Batch(cmds...)
}

func (m *Model) resizeColumns() {
	w := m.width
	if w < 40 {
		return
	}
	if strings.HasPrefix(strings.ToUpper(m.info.Mode), "TCP") {
		remoteW := w - 10 - 8 - 7 - 10 - 12
		if remoteW < 16 {
			remoteW = 16
		}
		m.table.SetColumns([]table.Column{
			{Title: "Time", Width: 10},
			{Title: "Proto", Width: 8},
			{Title: "Remote", Width: remoteW},
			{Title: "Status", Width: 7},
			{Title: "Duration", Width: 10},
		})
		return
	}
	timeW := 10
	methodW := 7
	statusW := 7
	durationW := 10
	pathW := w - timeW - methodW - statusW - durationW - 12
	if pathW < 10 {
		pathW = 10
	}
	m.table.SetColumns([]table.Column{
		{Title: "Time", Width: timeW},
		{Title: "Method", Width: methodW},
		{Title: "Path", Width: pathW},
		{Title: "Status", Width: statusW},
		{Title: "Duration", Width: durationW},
	})
}

func (m *Model) rebuildRows() {
	rows := make([]table.Row, len(m.requests))
	for i, r := range m.requests {
		rows[i] = table.Row{
			r.Time.Format("15:04:05"),
			r.Method,
			truncate(r.URL, 60),
			fmt.Sprintf("%d", r.StatusCode),
			formatDuration(r.Duration),
		}
	}
	m.table.SetRows(rows)
}

func (m *Model) updateDetail() {
	idx := m.table.Cursor()
	if idx < 0 || idx >= len(m.requests) {
		return
	}
	req := m.requests[idx]
	m.detail.SetContent(m.renderRequestDetail(req))
	m.detail.GotoTop()
}

func (m Model) View() string {
	if !m.ready {
		return appStyle.Render(emptyStateStyle.Render("warming up pierx..."))
	}

	header := m.renderHeader()
	divider := dividerStyle.Render(strings.Repeat("-", max(20, m.width-2)))

	var body string
	if m.view == viewList {
		if len(m.requests) == 0 {
			body = tableFrameStyle.Width(max(32, m.width-2)).Render(emptyStateStyle.Render("Waiting for requests..."))
		} else {
			body = tableFrameStyle.Width(max(32, m.width-2)).Render(m.table.View())
		}
	} else {
		m.updateDetailView()
		body = detailFrameStyle.Width(max(32, m.width-2)).Render(m.detail.View())
	}

	help := m.renderHelp()

	return appStyle.Render(lipgloss.JoinVertical(lipgloss.Left,
		header,
		divider,
		body,
		divider,
		help,
	))
}

func (m *Model) updateDetailView() {
	idx := m.table.Cursor()
	if idx < 0 || idx >= len(m.requests) {
		return
	}
	req := m.requests[idx]
	m.detail.SetContent(m.renderRequestDetail(req))
}

func (m Model) renderHeader() string {
	banner := headerTitleStyle.Render(strings.Join([]string{
		"  ▘      ",
		"▛▌▌█▌▛▘▚▘",
		"▙▌▌▙▖▌ ▞▖",
		"▌",
	}, "\n"))
	version := headerVersionStyle.Render("v" + m.info.Version)
	brand := lipgloss.JoinVertical(lipgloss.Left, banner, headerVersionStyle.Render("pajarori"))

	tunnel := labelStyle.Render("Tunnel") + urlStyle.Render(m.info.TunnelURL)
	mode := labelStyle.Render("Mode") + valueStyle.Render(m.info.Mode)
	local := labelStyle.Render("Local") + valueStyle.Render(m.info.LocalAddr)

	var access string
	if m.info.Auth == "anonymous" {
		access = labelStyle.Render("Access") + statusAnonymous.Render("anonymous")
	} else {
		access = labelStyle.Render("Access") + statusReserved.Render("reserved")
	}

	statusValue := statusConnected.Render(m.info.Status)
	if m.info.Status == "connecting" || m.info.Status == "reconnecting" {
		statusValue = statusAnonymous.Render(m.info.Status)
	}
	if m.info.Status == "disconnected" {
		statusValue = statusReserved.Render(m.info.Status)
	}
	status := labelStyle.Render("Status") + statusValue
	requests := labelStyle.Render("Requests") + metricStyle.Render(fmt.Sprintf("%d total", len(m.requests)))
	latency := labelStyle.Render("Latency") + valueStyle.Render(m.info.Latency)

	meta := []string{version, "", tunnel, local, mode, access, status, latency, requests}

	if m.info.PubAddr != "" {
		meta = append(meta, labelStyle.Render("Pub UDP")+valueStyle.Render(m.info.PubAddr))
	}

	content := lipgloss.JoinHorizontal(lipgloss.Top,
		lipgloss.NewStyle().Width(14).MarginRight(2).Render(brand),
		strings.Join(meta, "\n"),
	)
	return headerBoxStyle.Width(max(40, m.width-2)).Render(content)
}

func (m Model) renderRequestDetail(req *web.CapturedRequest) string {
	if req.Method == "TCP" {
		return lipgloss.JoinVertical(lipgloss.Left,
			detailHeaderStyle.Render("TCP Connection"),
			"",
			detailKeyStyle.Render("Remote: ")+detailValStyle.Render(req.URL),
			detailKeyStyle.Render("Time: ")+detailValStyle.Render(req.Time.Format("15:04:05.000")),
			detailKeyStyle.Render("Status: ")+statusCodeStyle(req.StatusCode).Render(fmt.Sprintf("%d", req.StatusCode)),
			detailKeyStyle.Render("Duration: ")+detailValStyle.Render(formatDuration(req.Duration)),
		)
	}
	title := detailHeaderStyle.Render(fmt.Sprintf("%s %s", req.Method, req.URL))
	summary := strings.Join([]string{
		detailKeyStyle.Render("Status: ") + statusCodeStyle(req.StatusCode).Render(fmt.Sprintf("%d", req.StatusCode)),
		detailKeyStyle.Render("Time:   ") + detailValStyle.Render(req.Time.Format("15:04:05.000")),
		detailKeyStyle.Render("Duration: ") + detailValStyle.Render(formatDuration(req.Duration)),
	}, "\n")

	left := []string{detailSectionStyle.Render("Request"), renderHeaderBlock(req.RequestHeaders)}
	if req.RequestBody != "" {
		left = append(left, "", detailKeyStyle.Render("Body"), detailValStyle.Render(req.RequestBody))
	}

	right := []string{detailSectionStyle.Render("Response"), renderHeaderBlock(req.ResponseHeaders)}
	if req.ResponseBody != "" {
		right = append(right, "", detailKeyStyle.Render("Body"), detailValStyle.Render(req.ResponseBody))
	}

	panelGap := 3
	panelWidth := max(20, (m.detail.Width-panelGap)/2)
	panelStyle := lipgloss.NewStyle().Width(panelWidth)
	content := lipgloss.JoinHorizontal(
		lipgloss.Top,
		panelStyle.Render(strings.Join(left, "\n")),
		lipgloss.NewStyle().Width(panelGap).Render(""),
		panelStyle.Render(strings.Join(right, "\n")),
	)

	return lipgloss.JoinVertical(lipgloss.Left, title, "", summary, "", content)
}

func renderHeaderBlock(headers http.Header) string {
	if len(headers) == 0 {
		return detailValStyle.Render("(none)")
	}
	keys := make([]string, 0, len(headers))
	for k := range headers {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	for _, k := range keys {
		vals := append([]string(nil), headers[k]...)
		sort.Strings(vals)
		for _, v := range vals {
			b.WriteString(detailKeyStyle.Render(k + ": "))
			b.WriteString(detailValStyle.Render(v))
			b.WriteString("\n")
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

func (m Model) renderHelp() string {
	if m.view == viewDetail {
		return helpBarStyle.Width(max(32, m.width-2)).Render(
			helpKeyStyle.Render("esc") + " back   " +
				helpKeyStyle.Render("j/k") + " scroll   " +
				helpKeyStyle.Render("q") + " quit",
		)
	}
	return helpBarStyle.Width(max(32, m.width-2)).Render(
		helpKeyStyle.Render("j/k") + " navigate   " +
			helpKeyStyle.Render("enter") + " details   " +
			helpKeyStyle.Render("c") + " clear   " +
			helpKeyStyle.Render("q") + " quit",
	)
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max-1] + "…"
}

func formatDuration(d time.Duration) string {
	if d < time.Millisecond {
		return fmt.Sprintf("%dµs", d.Microseconds())
	}
	if d < time.Second {
		return fmt.Sprintf("%dms", d.Milliseconds())
	}
	return fmt.Sprintf("%.1fs", d.Seconds())
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
