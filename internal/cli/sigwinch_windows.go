//go:build windows

package cli

import "os"

func notifyWindowResize(_ chan<- os.Signal) {
	// Windows does not have SIGWINCH; watch redraws on its periodic ticker.
}
