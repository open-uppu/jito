package cli

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/spf13/cobra"
)

// DoctorCheck represents a single diagnostic.
type DoctorCheck struct {
	Name    string
	Status  string // "ok", "warn", "fail"
	Message string
}

func newDoctorCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "doctor",
		Short: "Diagnose jito installation and dependencies",
		RunE: func(cmd *cobra.Command, args []string) error {
			checks := []DoctorCheck{
				checkBinary(),
				checkConfig(),
				checkEnv(),
				checkGit(),
				checkNetwork(),
			}

			fmt.Println("🩺 jito doctor\n")
			ok, warn, fail := 0, 0, 0
			for _, c := range checks {
				emoji := "✅"
				if c.Status == "warn" {
					emoji = "⚠️ "
					warn++
				} else if c.Status == "fail" {
					emoji = "❌"
					fail++
				} else {
					ok++
				}
				fmt.Printf("  %s  %s\n", emoji, c.Name)
				if c.Message != "" {
					fmt.Printf("      %s\n", c.Message)
				}
			}
			fmt.Printf("\nResults: %d ok · %d warn · %d fail\n", ok, warn, fail)
			if fail > 0 {
				return fmt.Errorf("doctor found %d issues", fail)
			}
			return nil
		},
	}
}

func checkBinary() DoctorCheck {
	path := binaryPath()
	if path == "" {
		return DoctorCheck{Name: "binary", Status: "fail", Message: "could not determine path"}
	}
	if _, err := os.Stat(path); err != nil {
		return DoctorCheck{Name: "binary", Status: "fail", Message: "not found at " + path}
	}
	return DoctorCheck{Name: "binary", Status: "ok", Message: path}
}

func checkConfig() DoctorCheck {
	home, _ := os.UserHomeDir()
	cfg := filepath.Join(home, ".jito", "config.yaml")
	if _, err := os.Stat(cfg); err != nil {
		return DoctorCheck{Name: "config", Status: "warn", Message: "not found — run `jito init`"}
	}
	return DoctorCheck{Name: "config", Status: "ok", Message: cfg}
}

func checkEnv() DoctorCheck {
	if os.Getenv("JITO_API_KEY") != "" || os.Getenv("MINIMAX_API_KEY") != "" || os.Getenv("OPENAI_API_KEY") != "" {
		return DoctorCheck{Name: "api key", Status: "ok", Message: "found in env"}
	}
	if os.Getenv("JITO_MOCK") == "1" {
		return DoctorCheck{Name: "api key", Status: "warn", Message: "using mock mode (JITO_MOCK=1)"}
	}
	return DoctorCheck{Name: "api key", Status: "warn", Message: "no key found — set JITO_API_KEY or use JITO_MOCK=1"}
}

func checkGit() DoctorCheck {
	_, err := exec.LookPath("git")
	if err != nil {
		return DoctorCheck{Name: "git", Status: "warn", Message: "not installed (worktree/sub-agent features disabled)"}
	}
	return DoctorCheck{Name: "git", Status: "ok", Message: "available"}
}

func checkNetwork() DoctorCheck {
	// quick HEAD request to detect offline
	if _, err := headURL("https://api.minimax.io/v1/models", 3); err != nil {
		return DoctorCheck{Name: "network", Status: "warn", Message: "offline or restricted"}
	}
	return DoctorCheck{Name: "network", Status: "ok", Message: "online"}
}