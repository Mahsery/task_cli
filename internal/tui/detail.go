package tui

func (m Model) getDetailContentLines(detailWidth int) []string {
	t := m.currentTask()
	if t == nil {
		return nil
	}
	return m.detailDoc(detailWidth).lines
}
