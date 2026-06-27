package cli

import (
	"context"
	"fmt"
	"time"

	"github.com/spf13/cobra"
	"github.com/uppu/jito/internal/heartbeat"
)

func newHeartbeatCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "heartbeat",
		Short: "Start 2-minute heartbeat logger (for sub-agent mandate)",
		RunE: func(cmd *cobra.Command, args []string) error {
			logDir, _ := cmd.Flags().GetString("log-dir")
			if logDir == "" {
				logDir = "~/.jito/heartbeat"
			}

			h, err := heartbeat.New(logDir)
			if err != nil {
				return err
			}
			defer h.Close()

			ctx, cancel := context.WithCancel(cmd.Context())
			defer cancel()

			fmt.Printf("💓 jito heartbeat started (logDir=%s)\n", logDir)
			fmt.Println("Press Ctrl+C to stop")

			taskID := "jito-main"
			if len(args) > 0 {
				taskID = args[0]
			}

			_ = h.Beat(taskID, "STARTED", "heartbeat daemon online")
			ticker := time.NewTicker(2 * time.Minute)
			defer ticker.Stop()

			for {
				select {
				case <-ctx.Done():
					_ = h.Beat(taskID, "STOPPED", "graceful shutdown")
					return nil
				case t := <-ticker.C:
					_ = h.BeatAt(t, taskID, "ALIVE", "2-minute heartbeat tick")
					fmt.Printf("  [%s] heartbeat sent\n", t.Format("15:04:05"))
				}
			}
		},
	}
}