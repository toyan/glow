// Package ui provides the main UI for the glow application.
package ui

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/glow/v3/utils"
	"github.com/charmbracelet/log"
)

const (
	statusMessageTimeout = time.Second * 3 // how long to show status messages like "Copied contents"
	ellipsis             = "…"
)

// NewProgram returns a new Tea program. The returned cleanup function
// restores the terminal state and must be called after the program exits.
func NewProgram(cfg Config, content string) (*tea.Program, func()) {
	log.Debug(
		"Starting glow",
		"glamour",
		cfg.GlamourEnabled,
	)

	m := newModel(cfg, content)

	if utils.TextSizingEnabled() {
		if tty, state, err := openRawTerminal(); err == nil {
			painter := newRawPainter(os.Stdout, tty, state, cfg.EnableMouse)
			prog := tea.NewProgram(
				paintModel{inner: m, painter: painter},
				tea.WithoutRenderer(),
				tea.WithInput(tty),
			)
			painter.watchResize(prog)
			return prog, painter.stop
		}
	}

	return tea.NewProgram(m), func() {}
}

type errMsg struct{ err error }

func (e errMsg) Error() string { return e.err.Error() }

type statusMessageTimeoutMsg struct{}

// Common stuff we'll need to access in all models.
type commonModel struct {
	cfg    Config
	width  int
	height int
	styles Styles
}

type model struct {
	common   *commonModel
	fatalErr error
	pager    pagerModel
}

func newModel(cfg Config, content string) tea.Model {
	common := commonModel{
		cfg:    cfg,
		styles: newStyles(true),
	}

	m := model{
		common: &common,
		pager:  newPagerModel(&common),
	}

	path := cfg.Path
	if path == "" && content != "" {
		m.pager.currentDocument = markdown{Body: content}
		return m
	}

	if path == "" {
		m.fatalErr = errors.New("missing markdown source")
		return m
	}

	info, err := os.Stat(path)
	if err != nil {
		log.Error("unable to stat file", "file", path, "error", err)
		m.fatalErr = err
		return m
	}
	if info.IsDir() {
		m.fatalErr = fmt.Errorf("%s is a directory, provide a markdown file", path)
		return m
	}

	cwd, _ := os.Getwd()
	m.pager.currentDocument = markdown{
		localPath: path,
		Note:      stripAbsolutePath(path, cwd),
	}
	data, err := os.ReadFile(path)
	if err != nil {
		log.Error("unable to read file", "file", path, "error", err)
		m.fatalErr = err
		return m
	}
	m.pager.currentDocument.Body = string(utils.RemoveFrontmatter(data))

	return m
}

func (m model) Init() tea.Cmd {
	return tea.Batch(
		tea.RequestBackgroundColor,
		renderWithGlamour(m.pager, m.pager.currentDocument.Body),
	)
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	// If there's been an error, any key exits
	if m.fatalErr != nil {
		if _, ok := msg.(tea.KeyPressMsg); ok {
			return m, tea.Quit
		}
	}

	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.BackgroundColorMsg:
		m.common.styles = newStyles(msg.IsDark())
	case tea.KeyPressMsg:
		switch msg.String() {
		case keyEsc, "q":
			return m, tea.Quit

		case "ctrl+z":
			return m, tea.Suspend

		// Ctrl+C always quits no matter where in the application you are.
		case "ctrl+c":
			return m, tea.Quit
		}

	// Window size is received when starting up and on every resize
	case tea.WindowSizeMsg:
		m.common.width = msg.Width
		m.common.height = msg.Height
		m.pager.setSize(msg.Width, msg.Height)

	case fetchedMarkdownMsg:
		// We've reloaded a markdown file's contents for rendering
		m.pager.currentDocument = *msg
		body := string(utils.RemoveFrontmatter([]byte(msg.Body)))
		cmds = append(cmds, renderWithGlamour(m.pager, body))

	case errMsg:
		m.fatalErr = msg.err
		return m, nil
	}

	newPagerModel, cmd := m.pager.update(msg)
	m.pager = newPagerModel
	cmds = append(cmds, cmd)

	return m, tea.Batch(cmds...)
}

func (m model) View() tea.View {
	var content string
	if m.fatalErr != nil {
		content = errorView(m.common.styles, m.fatalErr, true)
	} else {
		content = m.pager.View()
	}

	v := tea.NewView(content)
	v.AltScreen = true
	if m.common.cfg.EnableMouse {
		v.MouseMode = tea.MouseModeCellMotion
	}
	return v
}

func errorView(styles Styles, err error, fatal bool) string {
	exitMsg := "press any key to "
	if fatal {
		exitMsg += "exit"
	} else {
		exitMsg += "return"
	}
	s := fmt.Sprintf("%s\n\n%v\n\n%s",
		styles.errorTitleStyle.Render("ERROR"),
		err,
		styles.subtleStyle.Render(exitMsg),
	)
	return "\n" + indent(s, 3)
}

// COMMANDS

func waitForStatusMessageTimeout(t *time.Timer) tea.Cmd {
	return func() tea.Msg {
		<-t.C
		return statusMessageTimeoutMsg{}
	}
}

// ETC

func stripAbsolutePath(fullPath, cwd string) string {
	fp, _ := filepath.EvalSymlinks(fullPath)
	cp, _ := filepath.EvalSymlinks(cwd)
	return strings.ReplaceAll(fp, cp+string(os.PathSeparator), "")
}

// Lightweight version of reflow's indent function.
func indent(s string, n int) string {
	if n <= 0 || s == "" {
		return s
	}
	l := strings.Split(s, "\n")
	b := strings.Builder{}
	i := strings.Repeat(" ", n)
	for _, v := range l {
		fmt.Fprintf(&b, "%s%s\n", i, v)
	}
	return b.String()
}
