package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
)

func newInitCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "init",
		Short: "Initialize jito config in ~/.jito/",
		RunE: func(cmd *cobra.Command, args []string) error {
			home, err := os.UserHomeDir()
			if err != nil {
				return err
			}
			jitoDir := filepath.Join(home, ".jito")
			if err := os.MkdirAll(jitoDir, 0o755); err != nil {
				return err
			}

			cfgPath := filepath.Join(jitoDir, "config.yaml")
			if _, err := os.Stat(cfgPath); os.IsNotExist(err) {
				defaultCfg := `# jito config
provider:
  name: minimax
  base_url: https://api.minimax.io/v1
  model: MiniMax-M3
  api_key_env: JITO_API_KEY

fallback_providers:
  - name: openrouter
    base_url: https://openrouter.ai/api/v1
    model: anthropic/claude-3.5-sonnet

mode_default: universal

heartbeat:
  enabled: false
  interval_seconds: 120
  log_dir: ~/.jito/heartbeat
`
				if err := os.WriteFile(cfgPath, []byte(defaultCfg), 0o600); err != nil {
					return err
				}
				fmt.Printf("✅ Created %s\n", cfgPath)
			} else {
				fmt.Printf("ℹ️  %s already exists\n", cfgPath)
			}

			envPath := filepath.Join(jitoDir, ".env.example")
			if _, err := os.Stat(envPath); os.IsNotExist(err) {
				envExample := `# jito environment variables
JITO_API_KEY=sk-your-minimax-key-here
`
				if err := os.WriteFile(envPath, []byte(envExample), 0o600); err != nil {
					return err
				}
				fmt.Printf("✅ Created %s\n", envPath)
			}

			fmt.Println("\nNext steps:")
			fmt.Println("  1. export JITO_API_KEY=sk-...")
			fmt.Println("  2. jito run \"hello world\"")
			return nil
		},
	}
}