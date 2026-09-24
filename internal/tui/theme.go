package tui

import (
	"task_cli/internal/domain"

	"github.com/charmbracelet/lipgloss"
)

// Kanagawa Dragon Color Palette
const (
	// Backgrounds
	DragonBlack0 = "#0d0c0c"
	DragonBlack1 = "#12120f"
	DragonBlack2 = "#1D1C19"
	DragonBlack3 = "#181616" // main dark bg
	DragonBlack4 = "#282727"
	DragonBlack5 = "#393836" // borders & dividers
	SumiInk1     = "#1F1F28" // header & footer bg
	SumiInk2     = "#2A2A37" // cursor & selection bg
	SumiInk3     = "#363646"

	// Foregrounds & neutrals
	FujiWhite   = "#DCD7BA" // primary text
	DragonWhite = "#c5c9c5" // body text
	DragonGray  = "#9e9b93" // secondary text
	DragonGray2 = "#a6a69c"
	DragonGray3 = "#7a8382" // table headers & meta labels
	FujiGray    = "#8c8a80" // timestamps & subtle elements (WCAG AA compliant)

	// Accents & semantics
	WaveAqua2     = "#7AA89F" // open status, keybindings, links
	CarpYellow    = "#e6c384" // in_progress status, cursor arrow, highlights
	DragonGreen   = "#87a987" // done / success, diff additions
	DragonRed     = "#c4746e" // urgent, diff deletions, delete key
	SurimiOrange  = "#ffa066" // high priority
	DragonYellow  = "#c0a36e" // notes, active filter
	DragonBlue2   = "#8ba4b0" // task IDs, scope badges
	OniViolet     = "#957FB8" // tags
	SpringViolet1 = "#938AA9" // authors
	DragonOrange2 = "#e46876" // search mode active
)

// Theme encapsulates styled lipgloss components for Kanagawa Dragon
type Theme struct {
	Header           lipgloss.Style
	ScopeLocal       lipgloss.Style
	ScopeGlobal      lipgloss.Style
	CountOpen        lipgloss.Style
	CountProg        lipgloss.Style
	CountDone        lipgloss.Style
	SearchPrompt     lipgloss.Style
	FilterIndicator  lipgloss.Style
	ColHeader        lipgloss.Style
	Border           lipgloss.Style
	ActiveBorder     lipgloss.Style
	ActiveHeader     lipgloss.Style
	InactiveHeader   lipgloss.Style
	Cursor           lipgloss.Style
	SelectedMark     lipgloss.Style
	UnselectedMark   lipgloss.Style
	BatchBadge       lipgloss.Style
	RowSelected      lipgloss.Style
	RowNormal        lipgloss.Style
	TaskID           lipgloss.Style
	TaskTitle        lipgloss.Style
	Tag              lipgloss.Style
	Age              lipgloss.Style
	CommitSHA        lipgloss.Style
	DetailID         lipgloss.Style
	DetailTitle      lipgloss.Style
	DetailLabel      lipgloss.Style
	DetailDesc       lipgloss.Style
	DetailResolved   lipgloss.Style
	DiffStat         lipgloss.Style
	DiffAdd          lipgloss.Style
	DiffSub          lipgloss.Style
	DiffHunk         lipgloss.Style
	NoteHeader       lipgloss.Style
	NoteAuthor       lipgloss.Style
	NoteTime         lipgloss.Style
	NoteContent      lipgloss.Style
	FooterBg         lipgloss.Style
	FooterKey        lipgloss.Style
	FooterDesc       lipgloss.Style
	FooterStatus     lipgloss.Style
	ModalBox         lipgloss.Style
	ModalTitle       lipgloss.Style
	ModalPrompt      lipgloss.Style
	ModalKey         lipgloss.Style
	ModalDesc        lipgloss.Style
	ModalCancel      lipgloss.Style
}

func NewKanagawaTheme() Theme {
	return Theme{
		Header: lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color(FujiWhite)).
			Background(lipgloss.Color(SumiInk1)).
			Padding(0, 1),

		ScopeLocal: lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color(DragonBlue2)),

		ScopeGlobal: lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color(WaveAqua2)),

		CountOpen: lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color(WaveAqua2)),

		CountProg: lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color(CarpYellow)),

		CountDone: lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color(DragonGreen)),

		SearchPrompt: lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color(DragonOrange2)),

		FilterIndicator: lipgloss.NewStyle().
			Foreground(lipgloss.Color(DragonYellow)),

		ColHeader: lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color(DragonGray3)),

		Border: lipgloss.NewStyle().
			Foreground(lipgloss.Color(DragonBlack5)),

		ActiveBorder: lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color(CarpYellow)),

		ActiveHeader: lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color(CarpYellow)),

		InactiveHeader: lipgloss.NewStyle().
			Foreground(lipgloss.Color(DragonGray3)),

		Cursor: lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color(CarpYellow)),

		SelectedMark: lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color(CarpYellow)),

		UnselectedMark: lipgloss.NewStyle().
			Foreground(lipgloss.Color(DragonGray3)),

		BatchBadge: lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color(DragonBlack0)).
			Background(lipgloss.Color(CarpYellow)).
			Padding(0, 1),

		RowSelected: lipgloss.NewStyle().
			Bold(true).
			Background(lipgloss.Color(SumiInk2)).
			Foreground(lipgloss.Color(FujiWhite)),

		RowNormal: lipgloss.NewStyle().
			Foreground(lipgloss.Color(DragonWhite)),

		TaskID: lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color(DragonBlue2)),

		TaskTitle: lipgloss.NewStyle().
			Foreground(lipgloss.Color(FujiWhite)),

		Tag: lipgloss.NewStyle().
			Foreground(lipgloss.Color(OniViolet)),

		Age: lipgloss.NewStyle().
			Foreground(lipgloss.Color(FujiGray)),

		CommitSHA: lipgloss.NewStyle().
			Foreground(lipgloss.Color(DragonBlue2)),

		DetailID: lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color(DragonBlue2)),

		DetailTitle: lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color(FujiWhite)),

		DetailLabel: lipgloss.NewStyle().
			Foreground(lipgloss.Color(DragonGray3)),

		DetailDesc: lipgloss.NewStyle().
			Foreground(lipgloss.Color(DragonWhite)),

		DetailResolved: lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color(DragonGreen)),

		DiffStat: lipgloss.NewStyle().
			Foreground(lipgloss.Color(WaveAqua2)),

		DiffAdd: lipgloss.NewStyle().
			Foreground(lipgloss.Color(DragonGreen)),

		DiffSub: lipgloss.NewStyle().
			Foreground(lipgloss.Color(DragonRed)),

		DiffHunk: lipgloss.NewStyle().
			Foreground(lipgloss.Color(WaveAqua2)),

		NoteHeader: lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color(DragonYellow)),

		NoteAuthor: lipgloss.NewStyle().
			Foreground(lipgloss.Color(SpringViolet1)),

		NoteTime: lipgloss.NewStyle().
			Foreground(lipgloss.Color(FujiGray)),

		NoteContent: lipgloss.NewStyle().
			Foreground(lipgloss.Color(DragonWhite)),

		FooterBg: lipgloss.NewStyle().
			Background(lipgloss.Color(DragonBlack1)).
			Padding(0, 1),

		FooterKey: lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color(WaveAqua2)),

		FooterDesc: lipgloss.NewStyle().
			Foreground(lipgloss.Color(DragonGray)),

		FooterStatus: lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color(CarpYellow)),

		ModalBox: lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color(SurimiOrange)).
			Background(lipgloss.Color(DragonBlack2)).
			Padding(1, 2),

		ModalTitle: lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color(DragonBlack0)).
			Background(lipgloss.Color(SurimiOrange)).
			Padding(0, 1),

		ModalPrompt: lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color(FujiWhite)),

		ModalKey: lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color(WaveAqua2)),

		ModalDesc: lipgloss.NewStyle().
			Foreground(lipgloss.Color(DragonGray)),

		ModalCancel: lipgloss.NewStyle().
			Foreground(lipgloss.Color(DragonRed)),
	}
}

// StatusStyle returns color styling for a given task status.
func StatusStyle(s domain.Status) lipgloss.Style {
	switch s {
	case domain.StatusOpen:
		return lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(WaveAqua2))
	case domain.StatusInProgress:
		return lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(CarpYellow))
	case domain.StatusDone:
		return lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(DragonGreen))
	case domain.StatusClosed:
		return lipgloss.NewStyle().Foreground(lipgloss.Color(FujiGray))
	default:
		return lipgloss.NewStyle().Foreground(lipgloss.Color(DragonWhite))
	}
}

// PriorityStyle returns color styling for a given task priority.
func PriorityStyle(p domain.Priority) lipgloss.Style {
	switch p {
	case domain.PriorityLow:
		return lipgloss.NewStyle().Foreground(lipgloss.Color(DragonGray3))
	case domain.PriorityNormal:
		return lipgloss.NewStyle().Foreground(lipgloss.Color(DragonWhite))
	case domain.PriorityHigh:
		return lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(SurimiOrange))
	case domain.PriorityUrgent:
		return lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(DragonRed))
	default:
		return lipgloss.NewStyle().Foreground(lipgloss.Color(DragonWhite))
	}
}
