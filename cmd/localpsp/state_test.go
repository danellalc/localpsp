package main

import (
	"strings"
	"testing"
	"time"
)

func TestFormatStatePlainHasNoANSICodes(t *testing.T) {
	st := adminState{
		Customers: []adminCustomer{{ID: "cus_1", Name: "Ana", Email: "ana@example.com", CreatedAt: time.Now()}},
		Charges: []adminCharge{
			{ID: "pay_1", CustomerID: "cus_1", Status: "overdue", Amount: 15000, DueDate: time.Now(), CreatedAt: time.Now(), UpdatedAt: time.Now()},
		},
		Webhooks: []adminWebhookState{
			{ID: "wh_1", URL: "http://example.test/hook", Interrupted: true, ConsecutiveFailures: 15, Pending: 4},
		},
		DeliveryLog: []adminDeliveryAttempt{
			{EventID: "evt_1", Endpoint: "wh_1", At: time.Now(), Status: 500, Success: false},
		},
	}

	out := formatState(st, false)

	if strings.Contains(out, "\x1b[") {
		t.Errorf("formatState(color=false) contains an ANSI escape code:\n%s", out)
	}
}

func TestFormatStateColorHighlightsInterruptedQueue(t *testing.T) {
	st := adminState{
		Webhooks: []adminWebhookState{
			{ID: "wh_1", URL: "http://example.test/hook", Interrupted: true, ConsecutiveFailures: 15, Pending: 4},
		},
	}

	out := formatState(st, true)

	if !strings.Contains(out, "INTERRUPTED") {
		t.Errorf("formatState(color=true) missing an unmissable INTERRUPTED marker:\n%s", out)
	}
	if !strings.Contains(out, "\x1b[") {
		t.Errorf("formatState(color=true) contains no ANSI escape codes:\n%s", out)
	}
}

func TestFormatStateReportsCounts(t *testing.T) {
	st := adminState{
		Customers:     []adminCustomer{{ID: "cus_1", Name: "Ana"}, {ID: "cus_2", Name: "Bea"}},
		Charges:       []adminCharge{{ID: "pay_1", CustomerID: "cus_1", Status: "created", Amount: 1000}},
		Subscriptions: nil,
	}

	out := formatState(st, false)

	if !strings.Contains(out, "2 customer(s)") {
		t.Errorf("formatState() missing customer count:\n%s", out)
	}
	if !strings.Contains(out, "1 charge(s)") {
		t.Errorf("formatState() missing charge count:\n%s", out)
	}
	if !strings.Contains(out, "cus_1") || !strings.Contains(out, "pay_1") {
		t.Errorf("formatState() missing expected ids:\n%s", out)
	}
	if !strings.Contains(out, "R$ 10.00") {
		t.Errorf("formatState() missing formatted amount:\n%s", out)
	}
}

func TestFormatStateEmptySectionsSayNoneYet(t *testing.T) {
	out := formatState(adminState{}, false)

	if !strings.Contains(out, "none yet") {
		t.Errorf("formatState() with no data should say none yet:\n%s", out)
	}
	if !strings.Contains(out, "none registered") {
		t.Errorf("formatState() with no webhooks should say none registered:\n%s", out)
	}
}

func TestFormatStateShowsOnlyRecentDeliveries(t *testing.T) {
	log := make([]adminDeliveryAttempt, 0, 20)
	for i := 0; i < 20; i++ {
		log = append(log, adminDeliveryAttempt{
			EventID: "evt_" + string(rune('a'+i)), Endpoint: "wh_1", At: time.Now(), Status: 200, Success: true,
		})
	}
	st := adminState{DeliveryLog: log}

	out := formatState(st, false)

	oldest := "evt_" + string(rune('a'))
	newest := "evt_" + string(rune('a'+19))
	if strings.Contains(out, oldest) {
		t.Errorf("formatState() shows the oldest delivery, want only the most recent %d", recentDeliveries)
	}
	if !strings.Contains(out, newest) {
		t.Errorf("formatState() missing the most recent delivery:\n%s", out)
	}
}

// TestTableAlignsAccentedNames guards against a real bug caught in
// review: table() sized the first column with len() (a UTF-8 byte
// count), which overstates the width of any cell with an accented
// character, routine in Portuguese names like "José" or "Conceição",
// this tool's actual audience. That pushed the second column's start
// position out of alignment on every row containing one.
func TestTableAlignsAccentedNames(t *testing.T) {
	var b strings.Builder
	table(&b, []string{"NAME", "EMAIL"}, [][]string{
		{"José", "jose@example.test"},
		{"Bea", "bea@example.test"},
	}, nil)

	lines := strings.Split(strings.TrimRight(b.String(), "\n"), "\n")
	if len(lines) != 3 {
		t.Fatalf("got %d lines, want 3 (header + 2 rows):\n%q", len(lines), b.String())
	}

	// secondColumnStart returns the rune index where the second column's
	// text begins: past the first field, past the run of spaces after it.
	secondColumnStart := func(line string) int {
		runes := []rune(line)
		i := 0
		for i < len(runes) && runes[i] != ' ' {
			i++
		}
		for i < len(runes) && runes[i] == ' ' {
			i++
		}
		return i
	}
	// "José" is 4 visible characters but 5 bytes (é is two bytes in
	// UTF-8), the widest of the three column-0 values ("NAME", "José",
	// "Bea") either way; a byte-length width would compute 5 here instead
	// of 4, pushing this row's EMAIL column one character later than the
	// other two.
	want := secondColumnStart(lines[0])
	for i, line := range lines[1:] {
		if got := secondColumnStart(line); got != want {
			t.Errorf("row %d: second column starts at rune %d, want %d (misaligned): %q", i, got, want, line)
		}
	}
}

func TestFormatAmount(t *testing.T) {
	tests := []struct {
		cents int64
		want  string
	}{
		{0, "R$ 0.00"},
		{1990, "R$ 19.90"},
		{100, "R$ 1.00"},
		{1, "R$ 0.01"},
	}
	for _, tt := range tests {
		if got := formatAmount(tt.cents); got != tt.want {
			t.Errorf("formatAmount(%d) = %q, want %q", tt.cents, got, tt.want)
		}
	}
}
