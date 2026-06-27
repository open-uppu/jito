package cli

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/uppu/jito/internal/mode"
	"github.com/uppu/jito/internal/provider"
)

func newRunCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "run [prompt]",
		Short: "Run a single prompt (non-interactive)",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			modeName, _ := cmd.Flags().GetString("mode")
			modelOverride, _ := cmd.Flags().GetString("model")
			verbose, _ := cmd.Flags().GetBool("verbose")

			m, err := mode.Get(modeName)
			if err != nil {
				return err
			}

			prompt := args[0]
			systemPrompt := m.SystemPrompt()

			if verbose {
				fmt.Printf("[jito] mode=%s model=%s\n", m.Name(), modelOrDefault(modelOverride))
				fmt.Printf("[jito] system=%d chars prompt=%d chars\n", len(systemPrompt), len(prompt))
			}

			// Build provider
			p, err := provider.NewFromConfig(modelOverride)
			if err != nil {
				return fmt.Errorf("provider init: %w", err)
			}

			// Call provider
			resp, err := p.Chat(cmd.Context(), systemPrompt, prompt)
			if err != nil {
				return fmt.Errorf("provider call: %w", err)
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