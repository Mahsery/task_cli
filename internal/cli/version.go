package cli

import (
	"encoding/json"
	"fmt"
	"os"

	"task_cli/internal/version"

	"github.com/spf13/cobra"
)

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print tk version and build details",
	Long:  "Display detailed version information including semantic version, git commit SHA, build date, and runtime platform.",
	Example: `  tk version
  tk version --json
  tk --version`,
	RunE: func(_ *cobra.Command, _ []string) error {
		info := version.Get()
		if jsonFlag {
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			return enc.Encode(info)
		}
		fmt.Printf("tk version %s (commit: %s, built: %s, %s)\n",
			info.Version, info.Commit, info.BuildDate, info.Platform)
		return nil
	},
}

func init() {
	RootCmd.AddCommand(versionCmd)
}
