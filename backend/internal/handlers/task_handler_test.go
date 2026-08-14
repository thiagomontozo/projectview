package handlers

import (
	"testing"

	"projectview/internal/models"
)

func TestDefaultPriority(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{models.PriorityLow, models.PriorityLow},
		{models.PriorityMedium, models.PriorityMedium},
		{models.PriorityHigh, models.PriorityHigh},
		{models.PriorityUrgent, models.PriorityUrgent},
		// Anything the UI did not send, or an unknown value from an API
		// client, settles on "medium" rather than being stored verbatim.
		{"", models.PriorityMedium},
		{"critical", models.PriorityMedium},
		{"HIGH", models.PriorityMedium},
	}

	for _, tc := range cases {
		if got := defaultPriority(tc.in); got != tc.want {
			t.Errorf("defaultPriority(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
