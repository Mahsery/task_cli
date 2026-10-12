package tui

func (m Model) getDetailContentLines(detailWidth int) []string {
	if len(m.tasks) == 0 || m.cursor >= len(m.tasks) {
		return nil
	}
	t := m.currentTask()
	if t == nil {
		return nil
	}
	return m.detailDoc(detailWidth).lines
}
