package tui

import (
	"context"
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/deppes/localsend-cli/internal/handlers"
)

type receiveState int

const (
	stateWaiting receiveState = iota
	statePrompted
	stateReceiving
	stateDone
	stateError
)

type incomingMsg handlers.IncomingTransfer

// doneMsg is a struct (not an interface alias) so that doneMsg{nil} is a
// non-nil tea.Msg and the type switch fires correctly on success.
type doneMsg struct{ err error }

// ReceiveModel is the Bubble Tea model for receive mode.
type ReceiveModel struct {
	state    receiveState
	incoming *handlers.IncomingTransfer
	logs     []logLine
	updates  <-chan handlers.IncomingTransfer
	Err      error
}

func NewReceiveView(updates <-chan handlers.IncomingTransfer) ReceiveModel {
	return ReceiveModel{
		state:   stateWaiting,
		updates: updates,
		logs:    []logLine{{t: time.Now(), msg: "listening for incoming transfers..."}},
	}
}

func (m ReceiveModel) Init() tea.Cmd {
	return tea.Batch(tick(), waitForIncoming(m.updates))
}

func (m ReceiveModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch m.state {
		case stateWaiting, stateReceiving, stateDone, stateError:
			if msg.String() == "q" || msg.String() == "ctrl+c" {
				if m.incoming != nil {
					select {
					case m.incoming.Accept <- false:
					default:
					}
				}
				return m, tea.Quit
			}

		case statePrompted:
			switch msg.String() {
			case "y", "Y":
				m.incoming.Accept <- true
				m.state = stateReceiving
				m.logs = append(m.logs, logLine{t: time.Now(), msg: "accepted — receiving..."})
				return m, waitForDone(m.incoming.Done)
			case "n", "N", "ctrl+c", "q":
				m.incoming.Accept <- false
				m.state = stateWaiting
				m.incoming = nil
				m.logs = append(m.logs, logLine{t: time.Now(), msg: "rejected"})
				return m, waitForIncoming(m.updates)
			}
		}

	case incomingMsg:
		t := handlers.IncomingTransfer(msg)
		m.incoming = &t
		if t.IsFav {
			// Accept was pre-filled by the handler; Incoming() already consumed it.
			// Just update state and wait for the transfer to finish.
			m.state = stateReceiving
			m.logs = append(m.logs, logLine{
				t:   time.Now(),
				msg: fmt.Sprintf("auto-accepting from %s (favorite)", t.Alias),
			})
			return m, waitForDone(t.Done)
		}
		m.state = statePrompted
		m.logs = append(m.logs, logLine{
			t:   time.Now(),
			msg: fmt.Sprintf("incoming from %s (%s) — %d file(s)", t.Alias, t.FromIP, len(t.Files)),
		})
		return m, nil

	case doneMsg:
		if msg.err != nil {
			m.state = stateError
			m.Err = msg.err
			m.logs = append(m.logs, logLine{t: time.Now(), msg: styleBad.Render("error: " + msg.err.Error())})
		} else {
			m.state = stateDone
			m.logs = append(m.logs, logLine{t: time.Now(), msg: styleGood.Render("transfer complete")})
		}
		return m, tea.Quit

	case tickMsg:
		return m, tick()
	}

	return m, nil
}

func (m ReceiveModel) View() string {
	var b strings.Builder

	b.WriteString(styleTitle.Render("localsend-cli") + styleMuted.Render(" · receive"))
	b.WriteString("\n\n")

	switch m.state {
	case stateWaiting:
		b.WriteString(styleMuted.Render("  waiting for incoming transfer..."))
	case statePrompted:
		if m.incoming != nil {
			b.WriteString(fmt.Sprintf("  incoming from %s\n\n", m.incoming.Alias))
			b.WriteString("  Accept? [y/n]: ")
		}
	case stateReceiving:
		b.WriteString(styleGood.Render("  receiving..."))
	case stateDone:
		b.WriteString(styleGood.Render("  done"))
	case stateError:
		b.WriteString(styleBad.Render("  error: " + m.Err.Error()))
	}

	b.WriteString("\n\n")
	b.WriteString(styleDivider.Render(strings.Repeat("─", 48)))
	b.WriteString("\n")

	start := 0
	if len(m.logs) > 8 {
		start = len(m.logs) - 8
	}
	for _, l := range m.logs[start:] {
		ts := styleLogTime.Render(l.t.Format("15:04:05"))
		b.WriteString(fmt.Sprintf("%s  %s\n", ts, styleLog.Render(l.msg)))
	}

	if m.state != statePrompted {
		b.WriteString("\n")
		b.WriteString(styleMuted.Render("q quit"))
	}

	return b.String()
}

func waitForIncoming(ch <-chan handlers.IncomingTransfer) tea.Cmd {
	return func() tea.Msg {
		return incomingMsg(<-ch)
	}
}

func waitForDone(ch chan handlers.SessionResult) tea.Cmd {
	return func() tea.Msg {
		return doneMsg{(<-ch).Err}
	}
}

// ReceiveNotifier bridges the HTTP handler layer to the Bubble Tea receive view.
type ReceiveNotifier struct {
	ch chan handlers.IncomingTransfer
}

func NewReceiveNotifier() *ReceiveNotifier {
	return &ReceiveNotifier{ch: make(chan handlers.IncomingTransfer, 1)}
}

func (n *ReceiveNotifier) Incoming(ctx context.Context, t handlers.IncomingTransfer) bool {
	select {
	case n.ch <- t:
	case <-ctx.Done():
		return false
	}
	select {
	case accepted := <-t.Accept:
		return accepted
	case <-ctx.Done():
		return false
	}
}

func (n *ReceiveNotifier) Channel() <-chan handlers.IncomingTransfer {
	return n.ch
}
