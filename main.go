// Package main provides the entry point for the Glow CLI application.
package main

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"mvdan.cc/sh/v3/shell"

	"charm.land/glamour/v2"
	"charm.land/glamour/v2/styles"
	"charm.land/glow/v3/ui"
	"charm.land/glow/v3/utils"
	"github.com/caarlos0/env/v11"
	"github.com/charmbracelet/log"
	gap "github.com/muesli/go-app-paths"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"golang.org/x/term"
)

var (
	// Version as provided by goreleaser.
	Version = ""
	// CommitSHA as provided by goreleaser.
	CommitSHA = ""

	configFile       string
	pager            bool
	style            string
	width            uint
	showLineNumbers  bool
	preserveNewLines bool
	mouse            bool

	rootCmd = &cobra.Command{
		Use:   "glow [FILE]",
		Short: "Render markdown on the CLI, with pizzazz!",
		Long: paragraph(
			fmt.Sprintf("\nRender markdown on the CLI, %s!", keyword("with pizzazz")),
		),
		SilenceErrors:    false,
		SilenceUsage:     true,
		TraverseChildren: true,
		Args:             cobra.MaximumNArgs(1),
		ValidArgsFunction: func(*cobra.Command, []string, string) ([]string, cobra.ShellCompDirective) {
			return nil, cobra.ShellCompDirectiveDefault
		},
		PersistentPreRunE: func(cmd *cobra.Command, _ []string) error {
			return validateOptions(cmd)
		},
		RunE: execute,
	}
)

// source provides a readable markdown source.
type source struct {
	reader io.ReadCloser
	URL    string
}

// sourceFromArg parses an argument and creates a readable source for it.
func sourceFromArg(arg string) (*source, error) {
	// from stdin
	if arg == "-" {
		return &source{reader: os.Stdin}, nil
	}

	r, err := os.Open(arg)
	if err != nil {
		return nil, fmt.Errorf("unable to open file: %w", err)
	}
	u, err := filepath.Abs(arg)
	if err != nil {
		return nil, fmt.Errorf("unable to get absolute path: %w", err)
	}
	return &source{r, u}, nil
}

// validateStyle checks if the style is a default style, if not, checks that
// the custom style exists.
func validateStyle(style string) error {
	if style != "auto" && styles.DefaultStyles[style] == nil {
		style = utils.ExpandPath(style)
		if _, err := os.Stat(style); errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("specified style does not exist: %s", style)
		} else if err != nil {
			return fmt.Errorf("unable to stat file: %w", err)
		}
	}
	return nil
}

func validateOptions(cmd *cobra.Command) error {
	// grab config values from Viper
	width = viper.GetUint("width")
	mouse = viper.GetBool("mouse")
	pager = viper.GetBool("pager")
	preserveNewLines = viper.GetBool("preserveNewLines")
	showLineNumbers = viper.GetBool("showLineNumbers")

	// validate the glamour style
	style = viper.GetString("style")
	if err := validateStyle(style); err != nil {
		return err
	}

	isTerminal := term.IsTerminal(int(os.Stdout.Fd()))
	// We want to use a special no-TTY style, when stdout is not a terminal
	// and there was no specific style passed by arg
	if !isTerminal && !cmd.Flags().Changed("style") {
		style = "notty"
	}

	// Detect terminal width
	if !cmd.Flags().Changed("width") { //nolint:nestif
		if isTerminal && width == 0 {
			w, _, err := term.GetSize(int(os.Stdout.Fd()))
			if err == nil {
				width = uint(w) //nolint:gosec
			}

			if width > 120 {
				width = 120
			}
		}
		if width == 0 {
			width = 80
		}
	}
	return nil
}

func stdinIsPipe() (bool, error) {
	stat, err := os.Stdin.Stat()
	if err != nil {
		return false, fmt.Errorf("unable to open file: %w", err)
	}
	if stat.Mode()&os.ModeCharDevice == 0 || stat.Size() > 0 {
		return true, nil
	}
	return false, nil
}

func execute(cmd *cobra.Command, args []string) error {
	// if stdin is a pipe then use stdin for input. note that you can also
	// explicitly use a - to read from stdin.
	if yes, err := stdinIsPipe(); err != nil {
		return err
	} else if yes {
		src := &source{reader: os.Stdin}
		defer src.reader.Close() //nolint:errcheck
		return executeCLI(cmd, src, os.Stdout)
	}

	switch len(args) {
	case 0:
		return errors.New("missing markdown source")
	default:
		for _, arg := range args {
			if err := executeArg(cmd, arg, os.Stdout); err != nil {
				return err
			}
		}
	}

	return nil
}

func executeArg(cmd *cobra.Command, arg string, w io.Writer) error {
	// create an io.Reader from the markdown source in cli-args
	src, err := sourceFromArg(arg)
	if err != nil {
		return err
	}
	defer src.reader.Close() //nolint:errcheck
	return executeCLI(cmd, src, w)
}

func executeCLI(cmd *cobra.Command, src *source, w io.Writer) error {
	b, err := io.ReadAll(src.reader)
	if err != nil {
		return fmt.Errorf("unable to read from reader: %w", err)
	}

	b = utils.RemoveFrontmatter(b)

	isCode := !utils.IsMarkdownFile(src.URL)

	// initialize glamour
	r, err := glamour.NewTermRenderer(
		utils.GlamourStyle(style, isCode),
		glamour.WithWordWrap(int(width)), //nolint:gosec
		glamour.WithPreservedNewLines(),
	)
	if err != nil {
		return fmt.Errorf("unable to create renderer: %w", err)
	}

	content := string(b)
	ext := filepath.Ext(src.URL)
	if isCode {
		content = utils.WrapCodeBlock(string(b), ext)
	}

	out, err := r.Render(content)
	if err != nil {
		return fmt.Errorf("unable to render markdown: %w", err)
	}

	out = utils.ApplyTextSizing(out)

	// display
	if pager || cmd.Flags().Changed("pager") {
		pagerCmd := os.Getenv("PAGER")
		if pagerCmd == "" {
			pagerCmd = "less -r"
		}

		fields, err := shell.Fields(pagerCmd, os.Getenv)
		if err != nil || len(fields) == 0 {
			return fmt.Errorf("unable to parse PAGER command: %s", pagerCmd)
		}
		c := exec.Command(fields[0], fields[1:]...) //nolint:gosec
		c.Stdin = strings.NewReader(out)
		c.Stdout = os.Stdout
		if err := c.Run(); err != nil {
			return fmt.Errorf("unable to run command: %w", err)
		}
		return nil
	}

	// The TUI viewer is the default when stdout is a terminal; otherwise
	// print the rendered output so pipes and redirects keep working.
	if term.IsTerminal(int(os.Stdout.Fd())) {
		return runTUI(src.URL, content)
	}

	if _, err = fmt.Fprint(w, out); err != nil {
		return fmt.Errorf("unable to write to writer: %w", err)
	}
	return nil
}

func runTUI(path string, content string) error {
	// detect text sizing support before bubbletea takes over the terminal
	_ = utils.TextSizingEnabled()

	// Read environment to get debugging stuff
	cfg, err := env.ParseAs[ui.Config]()
	if err != nil {
		return fmt.Errorf("error parsing config: %v", err)
	}

	// use style set in env, or auto if unset
	if err := validateStyle(cfg.GlamourStyle); err != nil {
		cfg.GlamourStyle = style
	}

	cfg.Path = path
	cfg.ShowLineNumbers = showLineNumbers
	cfg.GlamourMaxWidth = width
	cfg.EnableMouse = mouse
	cfg.PreserveNewLines = preserveNewLines

	// Run Bubble Tea program
	prog, cleanup := ui.NewProgram(cfg, content)
	defer cleanup()
	if _, err := prog.Run(); err != nil {
		return fmt.Errorf("unable to run tui program: %w", err)
	}

	return nil
}

func main() {
	closer, err := setupLog()
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
	if err := rootCmd.Execute(); err != nil {
		_ = closer()
		os.Exit(1)
	}
	_ = closer()
}

func init() {
	tryLoadConfigFromDefaultPlaces()
	if len(CommitSHA) >= 7 {
		vt := rootCmd.VersionTemplate()
		rootCmd.SetVersionTemplate(vt[:len(vt)-1] + " (" + CommitSHA[0:7] + ")\n")
	}
	if Version == "" {
		Version = "unknown (built from source)"
	}
	rootCmd.Version = Version
	rootCmd.InitDefaultCompletionCmd()

	// "Glow Classic" cli arguments
	rootCmd.PersistentFlags().StringVar(&configFile, "config", "", fmt.Sprintf("config file (default %s)", viper.GetViper().ConfigFileUsed()))
	rootCmd.Flags().BoolVarP(&pager, "pager", "p", false, "display with pager")
	rootCmd.Flags().StringVarP(&style, "style", "s", "auto", "style name or JSON path")
	rootCmd.Flags().UintVarP(&width, "width", "w", 0, "word-wrap at width (set to 0 to disable)")
	rootCmd.Flags().BoolVarP(&showLineNumbers, "line-numbers", "l", false, "show line numbers (TUI-mode only)")
	rootCmd.Flags().BoolVarP(&preserveNewLines, "preserve-new-lines", "n", false, "preserve newlines in the output")
	rootCmd.Flags().BoolVarP(&mouse, "mouse", "m", false, "enable mouse wheel (TUI-mode only)")
	_ = rootCmd.Flags().MarkHidden("mouse")
	// Config bindings
	_ = viper.BindPFlag("pager", rootCmd.Flags().Lookup("pager"))
	_ = viper.BindPFlag("style", rootCmd.Flags().Lookup("style"))
	_ = viper.BindPFlag("width", rootCmd.Flags().Lookup("width"))
	_ = viper.BindPFlag("debug", rootCmd.Flags().Lookup("debug"))
	_ = viper.BindPFlag("mouse", rootCmd.Flags().Lookup("mouse"))
	_ = viper.BindPFlag("preserveNewLines", rootCmd.Flags().Lookup("preserve-new-lines"))
	_ = viper.BindPFlag("showLineNumbers", rootCmd.Flags().Lookup("line-numbers"))

	viper.SetDefault("style", "auto")
	viper.SetDefault("width", 0)

}

func tryLoadConfigFromDefaultPlaces() {
	scope := gap.NewScope(gap.User, "glow")
	dirs, err := scope.ConfigDirs()
	if err != nil {
		fmt.Println("Could not load find configuration directory.")
		os.Exit(1)
	}

	if c := os.Getenv("XDG_CONFIG_HOME"); c != "" {
		dirs = append([]string{filepath.Join(c, "glow")}, dirs...)
	}

	if c := os.Getenv("GLOW_CONFIG_HOME"); c != "" {
		dirs = append([]string{c}, dirs...)
	}

	for _, v := range dirs {
		viper.AddConfigPath(v)
	}

	viper.SetConfigName("glow")
	viper.SetConfigType("yaml")
	viper.SetEnvPrefix("glow")
	viper.AutomaticEnv()

	if err := viper.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
			log.Warn("Could not parse configuration file", "err", err)
		}
	}

	if used := viper.ConfigFileUsed(); used != "" {
		log.Debug("Using configuration file", "path", viper.ConfigFileUsed())
		return
	}

	if viper.ConfigFileUsed() == "" {
		configFile = filepath.Join(dirs[0], "glow.yml")
	}
}
