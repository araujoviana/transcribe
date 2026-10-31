package main

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

func stepLabel(s step, active bool) string {
	switch s {
	case stPrepare:
		if active {
			return "Baixando áudio"
		}
		return "Preparando áudio"
	case stCreate:
		return "Criando VM com GPU"
	case stInstall:
		return "Instalando WhisperX"
	case stUpload:
		return "Enviando áudio"
	case stTranscribe:
		return "Transcrevendo"
	case stDownload:
		return "Baixando transcrição"
	case stCleanup:
		return "Limpando (destruindo VM)"
	}
	return "?"
}

type (
	startMsg  step
	doneStep  step
	detailMsg struct {
		s step
		l string
	}
	finishMsg struct {
		out string
		err error
	}
)

type stepState struct {
	started, ended time.Time
	status         int // 0 pendente, 1 rodando, 2 ok
}

type model struct {
	spin       spinner.Model
	steps      [numSteps]stepState
	isURL      bool
	begin      time.Time
	detail     [numSteps]string
	cancel     func()
	cancelling bool
	finished   bool
	out        string
	err        error
}

var (
	cOK     = lipgloss.NewStyle().Foreground(lipgloss.Color("42"))
	cFail   = lipgloss.NewStyle().Foreground(lipgloss.Color("203")).Bold(true)
	cDim    = lipgloss.NewStyle().Foreground(lipgloss.Color("244"))
	cActive = lipgloss.NewStyle().Foreground(lipgloss.Color("212")).Bold(true)
	cTitle  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("99"))
)

func newModel(isURL bool, cancel func()) model {
	sp := spinner.New(spinner.WithSpinner(spinner.MiniDot))
	sp.Style = cActive
	return model{spin: sp, isURL: isURL, begin: time.Now(), cancel: cancel}
}

func (m model) Init() tea.Cmd { return m.spin.Tick }

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		if msg.String() == "ctrl+c" && !m.finished {
			m.cancelling = true
			m.cancel()
		}
	case startMsg:
		m.steps[msg].status, m.steps[msg].started, m.detail[msg] = 1, time.Now(), ""
	case doneStep:
		m.steps[msg].status, m.steps[msg].ended = 2, time.Now()
	case detailMsg:
		m.detail[msg.s] = msg.l
	case finishMsg:
		m.finished, m.out, m.err = true, msg.out, msg.err
		return m, tea.Quit
	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spin, cmd = m.spin.Update(msg)
		return m, cmd
	}
	return m, nil
}

func fmtDur(d time.Duration) string {
	d = d.Round(time.Second)
	if d >= time.Minute {
		return fmt.Sprintf("%dm%02ds", int(d.Minutes()), int(d.Seconds())%60)
	}
	return fmt.Sprintf("%ds", int(d.Seconds()))
}

func (m model) failedStep() (step, bool) {
	var se *stepError
	if errors.As(m.err, &se) {
		return se.step, true
	}
	return 0, false
}

func (m model) View() string {
	var b strings.Builder
	b.WriteString(cTitle.String() + "transcribe" + lipgloss.NewStyle().Render("") + "\n\n")
	failed, hasFail := m.failedStep()
	for i := step(0); i < numSteps; i++ {
		s := m.steps[i]
		label := stepLabel(i, m.isURL)
		switch {
		case m.finished && hasFail && failed == i:
			fmt.Fprintf(&b, " %s %s %s\n", cFail.Render("✗"), label, cDim.Render(fmtDur(time.Since(s.started))))
		case s.status == 2:
			fmt.Fprintf(&b, " %s %s %s\n", cOK.Render("✓"), label, cDim.Render(fmtDur(s.ended.Sub(s.started))))
		case s.status == 1 && !m.finished:
			fmt.Fprintf(&b, " %s %s %s\n", m.spin.View(), cActive.Render(label), cDim.Render(fmtDur(time.Since(s.started))))
			if d := m.detail[i]; d != "" {
				if r := []rune(d); len(r) > 90 {
					d = string(r[:90]) + "…"
				}
				fmt.Fprintf(&b, "     %s\n", cDim.Render(d))
			}
		case s.status == 1:
			fmt.Fprintf(&b, " %s %s\n", cDim.Render("·"), label)
		default:
			fmt.Fprintf(&b, " %s\n", cDim.Render("· "+label))
		}
	}
	if m.cancelling && !m.finished {
		b.WriteString("\n " + cFail.Render("Cancelando; aguarde a VM ser destruída…") + "\n")
	}
	fmt.Fprintf(&b, "\n %s\n", cDim.Render("tempo total "+fmtDur(time.Since(m.begin))))
	if m.finished {
		if m.err != nil {
			b.WriteString("\n" + cFail.Render(" Erro: "+m.err.Error()) + "\n")
			var se *stepError
			if errors.As(m.err, &se) && se.tail != "" {
				b.WriteString(cDim.Render(indent(se.tail)) + "\n")
			}
		} else {
			b.WriteString("\n " + cOK.Render("Pronto: ") + m.out + "\n")
		}
	}
	return b.String()
}

func indent(s string) string {
	return "   " + strings.ReplaceAll(s, "\n", "\n   ")
}

// teaReporter converte callbacks do pipeline em mensagens do bubbletea.
type teaReporter struct{ p *tea.Program }

func (r teaReporter) Start(s step)            { r.p.Send(startMsg(s)) }
func (r teaReporter) Done(s step)             { r.p.Send(doneStep(s)) }
func (r teaReporter) Detail(s step, l string) { r.p.Send(detailMsg{s, l}) }
