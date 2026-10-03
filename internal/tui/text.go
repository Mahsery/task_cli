package tui

import (
	"task_cli/internal/termutil"
)

var (
	wrapText          = termutil.WrapText
	timeAgo           = termutil.FormatAge
	isASCIIOnly       = termutil.IsASCIIOnly
	truncateToWidth   = termutil.Truncate
	padRight          = termutil.PadRight
	compactTreeBranch = termutil.CompactTreeBranch
)
