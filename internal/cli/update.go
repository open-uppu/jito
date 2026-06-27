package cli

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/spf13/cobra"
)

const githubReleasesAPI = "https://api.github.com/repos/uppu/jito/releases/latest"

type ghRelease struct {
	TagName string `json:"tag_name"`
	HTMLURL string `json:"html_url"`
}

func newUpdateCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "update",
		Short: "Check for updates and install the latest version",
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Println("🔍 Checking for updates...")

			client := &http.Client{Timeout: 10 * time.Second}
			resp, err := client.Get(githubReleasesAPI)
			if err != nil {
				fmt.Println("⚠️  Could not reach GitHub (offline mode)")
				fmt.Printf("📦 Current: jito %s (built %s)\n", curVersion, curDate)
				fmt.Println("💡 Run: cd /home/up-ubuntu/wokrspace/open-uppu/jito && git pull && go build")
				return nil
			}
			defer resp.Body.Close()

			if resp.StatusCode == 404 {
				fmt.Println("ℹ️  No releases published yet (private repo)")
				fmt.Printf("📦 Current: jito %s\n", curVersion)
				return nil
			}

			var release ghRelease
			if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
				return fmt.Errorf("parse release: %w", err)
			}

			fmt.Printf("📦 Current:  jito %s\n", curVersion)
			fmt.Printf("🆕 Latest:   jito %s\n", release.TagName)
			if release.TagName == "v"+curVersion {
				fmt.Println("✅ You're up to date!")
			} else {
				fmt.Printf("⬇️  Install: %s\n", release.HTMLURL)
			}
			return nil
		},
	}
}

// binaryPath returns the absolute path to the current jito binary.
func binaryPath() string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	abs, _ := filepath.Abs(exe)
	return abs
}

// archString returns a release asset suffix for current OS/arch.
func archString() string {
	return fmt.Sprintf("%s-%s", runtime.GOOS, runtime.GOARCH)
}

// curVersion / curDate are set by SetVersion (called from main).
var (
	curVersion = "dev"
	curDate    = "unknown"
)

// SetVersion injects version metadata from main.
func SetVersion(v, d string) {
	curVersion = v
	curDate = d
}