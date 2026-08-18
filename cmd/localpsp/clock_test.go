package main

import (
	"strings"
	"testing"
	"time"
)

func TestFormatClockAdvanceNothingDue(t *testing.T) {
	resp := clockAdvanceResponseBody{Now: time.Date(2026, 1, 4, 0, 0, 0, 0, time.UTC)}

	out := formatClockAdvance(resp)

	if !strings.Contains(out, "nothing due") {
		t.Errorf("formatClockAdvance() = %q, want it to say nothing due", out)
	}
	if !strings.Contains(out, "2026-01-04") {
		t.Errorf("formatClockAdvance() = %q, missing the new clock time", out)
	}
}

func TestFormatClockAdvanceListsTransitions(t *testing.T) {
	resp := clockAdvanceResponseBody{
		Now: time.Date(2026, 1, 4, 0, 0, 0, 0, time.UTC),
		Transitions: []clockTransition{
			{ChargeID: "pay_1", From: "created", To: "overdue", At: time.Date(2026, 1, 4, 0, 0, 0, 0, time.UTC)},
			{ChargeID: "pay_2", From: "created", To: "overdue", At: time.Date(2026, 1, 4, 0, 0, 0, 0, time.UTC)},
		},
	}

	out := formatClockAdvance(resp)

	if !strings.Contains(out, "pay_1: created -> overdue") {
		t.Errorf("formatClockAdvance() missing pay_1 transition:\n%s", out)
	}
	if !strings.Contains(out, "pay_2: created -> overdue") {
		t.Errorf("formatClockAdvance() missing pay_2 transition:\n%s", out)
	}
}
