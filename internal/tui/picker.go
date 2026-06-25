package tui

import (
	"fmt"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/deppes/localsend-cli/internal/protocol"
)

// DeviceUpdate is sent to the picker when a new device is discovered.
type DeviceUpdate struct {
	Device protocol.DiscoveredDevice
}

type tickMsg time.Time

// PickerModel is the Bubble Tea model for the device selection screen.
type PickerModel struct {
	devices  []protocol.DiscoveredDevice
	cursor   int
	logs     []logLine
	updates  <-chan protocol.DiscoveredDevice
	Selected *protocol.DiscoveredDevice
	Quit     bool
}

type logLine struct {
	t   time.Time
	msg string
}

func NewPicker(updates <-chan protocol.DiscoveredDevice) PickerModel {
	return PickerModel{
		updates: updates,
		logs:    []logLine{{t: time.Now(), msg: "discovering devices..."}},
	}
}

func (m PickerModel) Init() tea.Cmd {
	return tick()
}

func (m PickerModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c":
			m.Quit = true
			return m, tea.Quit
		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
			}
		case "down", "j":
			if m.cursor < len(m.devices)-1 {
				m.cursor++
			}
		case "g":
			m.cursor = 0
		case "G":
			if len(m.devices) > 0 {
				m.cursor = len(m.devices) - 1
			}
		case "enter":
			if len(m.devices) > 0 {
				selected := m.devices[m.cursor]
				m.Selected = &selected
				return m, tea.Quit
			}
		}

	case tickMsg:
		// Drain all pending device updates without blocking.
		for {
			select {
			case dev := <-m.updates:
				m.addDevice(dev)
			default:
				return m, tick()
			}
		}
	}

	return m, nil
}

func (m *PickerModel) addDevice(dev protocol.DiscoveredDevice) {
	// Update existing or append.
	for i, d := range m.devices {
		if d.IP == dev.IP {
			m.devices[i] = dev
			return
		}
	}
	m.devices = append(m.devices, dev)
	m.sortDevices()
	m.logs = append(m.logs, logLine{
		t:   time.Now(),
		msg: fmt.Sprintf("found %s (%s)", dev.Alias, dev.IP),
	})
}

func (m *PickerModel) sortDevices() {
	sort.SliceStable(m.devices, func(i, j int) bool {
		if m.devices[i].Favorite != m.devices[j].Favorite {
			return m.devices[i].Favorite
		}
		return m.devices[i].Alias < m.devices[j].Alias
	})
}

func (m PickerModel) View() string {
	var b strings.Builder

	b.WriteString(styleTitle.Render("localsend-cli"))
	b.WriteString("\n\n")

	if len(m.devices) == 0 {
		b.WriteString(styleMuted.Render("  scanning..."))
		b.WriteString("\n")
	} else {
		for i, dev := range m.devices {
			star := "  "
			if dev.Favorite {
				star = styleStar.Render("★ ")
			}

			name := dev.Alias
			ip := styleMuted.Render(dev.IP)

			line := fmt.Sprintf("%s%-24s %s", star, name, ip)

			if i == m.cursor {
				b.WriteString(styleCursor.Render("> ") + line)
			} else {
				b.WriteString("  " + line)
			}
			b.WriteString("\n")
		}
	}

	b.WriteString("\n")
	b.WriteString(styleDivider.Render(strings.Repeat("─", 48)))
	b.WriteString("\n")

	// Show last 6 log lines.
	start := 0
	if len(m.logs) > 6 {
		start = len(m.logs) - 6
	}
	for _, l := range m.logs[start:] {
		ts := styleLogTime.Render(l.t.Format("15:04:05"))
		b.WriteString(fmt.Sprintf("%s  %s\n", ts, styleLog.Render(l.msg)))
	}

	b.WriteString("\n")
	b.WriteString(styleMuted.Render("j/k navigate · enter send · q quit"))

	return b.String()
}

func (m PickerModel) AddLog(msg string) PickerModel {
	m.logs = append(m.logs, logLine{t: time.Now(), msg: msg})
	return m
}

func tick() tea.Cmd {
	return tea.Tick(100*time.Millisecond, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}
