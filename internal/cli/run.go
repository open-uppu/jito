package cli

import (
	"context"
	"fmt"
	"os"

	"github.com/spf13/cobra"
	jitocontext "github.com/uppu/jito/internal/context"
	"github.com/uppu/jito/internal/mode"
	"github.com/uppu/jito/internal/provider"
)

// RunOptions captures all inputs to ExecuteRun so the function is
// testable in isolation (without spawning cobra).
type RunOptions struct {
	Prompt   string
	ModeName string
	Model    string
	Verbose  bool
	CWD      string            // override os.Getwd() for tests
	Provider provider.Provider  // optional injected provider (tests)
	Loader   *jitocontext.Loader // optional pre-built loader (tests)
	Ctx      context.Context    // optional context for the LLM call
}

// ExecuteRun is the core of the `run` command. It loads JITO.md
// context, prepends it to the mode's system prompt, then calls the
// provider. Returns the provider's response string.
func ExecuteRun(opts RunOptions) (string, error) {
	m, err := mode.Get(opts.ModeName)
	if err != nil {
		return "", err
	}

	prompt := opts.Prompt
	systemPrompt := m.SystemPrompt()

	// Load JITO.md context (cwd + walk-up + $HOME/.jito/JITO.md)
	// and prepend it to the system prompt so the LLM sees the
	// same context the user expects.
	loader := opts.Loader
	if loader == nil {
		cwd := opts.CWD
		if cwd == "" {
			cwd, _ = os.Getwd()
		}
		l, lerr := jitocontext.NewLoader(cwd)
		if lerr == nil {
			loader = l
		}
	}
	if loader != nil {
		if _, loadErr := loader.Load(); loadErr == nil {
			if section := loader.SystemPromptSection(); section != "" {
				systemPrompt = systemPrompt + "\n" + section
			}
			if opts.Verbose {
				fmt.Printf("[jito] context: %d files loaded\n", loader.Count())
			}
		}
	}

	if opts.Verbose {
		fmt.Printf("[jito] mode=%s model=%s\n", m.Name(), modelOrDefault(opts.Model))
		fmt.Printf("[jito] system=%d chars prompt=%d chars\n", len(systemPrompt), len(prompt))
	}

	// Build provider
	p := opts.Provider
	if p == nil {
		p, err = provider.NewFromConfig(opts.Model)
		if err != nil {
			return "", fmt.Errorf("provider init: %w", err)
		}
	}

	ctx := opts.Ctx
	if ctx == nil {
		ctx = context.Background()
	}

	// Call provider
	resp, err := p.Chat(ctx, systemPrompt, prompt)
	if err != nil {
		return "", fmt.Errorf("provider call: %w", err)
	}
	return resp, nil
}

func newRunCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "run [prompt]",
		Short: "Run a single prompt (non-interactive)",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			modeName, _ := cmd.Flags().GetString("mode")
			modelOverride, _ := cmd.Flags().GetString("model")
			verbose, _ := cmd.Flags().GetBool("verbose")

			resp, err := ExecuteRun(RunOptions{
				Prompt:   args[0],
				ModeName: modeName,
				Model:    modelOverride,
				Verbose:  verbose,
				Ctx:      cmd.Context(),
			})
			if err != nil {
				return err
			}
			fmt.Println(resp)
			return nil
		},
	}
}

func modelOrDefault(override string) string {
	if override != "" {
		return override
	}
	return "minimax/MiniMax-M3"
}