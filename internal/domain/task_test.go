package domain

import (
	"testing"
)

func TestParseStatus(t *testing.T) {
	tests := []struct {
		input    string
		expected Status
		wantErr  bool
	}{
		{"open", StatusOpen, false},
		{"o", StatusOpen, false},
		{"OPEN", StatusOpen, false},
		{"in_progress", StatusInProgress, false},
		{"in-progress", StatusInProgress, false},
		{"prog", StatusInProgress, false},
		{"active", StatusInProgress, false},
		{"done", StatusDone, false},
		{"d", StatusDone, false},
		{"completed", StatusDone, false},
		{"closed", StatusClosed, false},
		{"c", StatusClosed, false},
		{"invalid_status", StatusOpen, true},
		{"", StatusOpen, true},
	}

	for _, tt := range tests {
		got, err := ParseStatus(tt.input)
		if (err != nil) != tt.wantErr {
			t.Errorf("ParseStatus(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
		}
		if !tt.wantErr && got != tt.expected {
			t.Errorf("ParseStatus(%q) = %v, want %v", tt.input, got, tt.expected)
		}
	}
}

func TestParsePriority(t *testing.T) {
	tests := []struct {
		input    string
		expected Priority
		wantErr  bool
	}{
		{"low", PriorityLow, false},
		{"l", PriorityLow, false},
		{"normal", PriorityNormal, false},
		{"norm", PriorityNormal, false},
		{"", PriorityNormal, false},
		{"high", PriorityHigh, false},
		{"h", PriorityHigh, false},
		{"urgent", PriorityUrgent, false},
		{"u", PriorityUrgent, false},
		{"critical", PriorityUrgent, false},
		{"extreme", PriorityNormal, true},
	}

	for _, tt := range tests {
		got, err := ParsePriority(tt.input)
		if (err != nil) != tt.wantErr {
			t.Errorf("ParsePriority(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
		}
		if !tt.wantErr && got != tt.expected {
			t.Errorf("ParsePriority(%q) = %v, want %v", tt.input, got, tt.expected)
		}
	}
}
