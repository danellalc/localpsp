package main

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/mattn/go-isatty"
)

// adminCustomer, adminCharge, adminSubscription, adminWebhookState and
// adminDeliveryAttempt mirror the JSON shape of GET /_localpsp/state
// (providers/asaas/admin.go). Kept as separate types here, not shared
// through an import, so the CLI depends only on the wire shape, not on
// providers/asaas internals.
type adminCustomer struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Email     string    `json:"email"`
	TaxID     string    `json:"taxId"`
	CreatedAt time.Time `json:"createdAt"`
}

type adminCharge struct {
	ID          string    `json:"id"`
	CustomerID  string    `json:"customerId"`
	BillingType string    `json:"billingType"`
	Amount      int64     `json:"amount"`
	Status      string    `json:"status"`
	DueDate     time.Time `json:"dueDate"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
}

type adminSubscription struct {
	ID          string    `json:"id"`
	CustomerID  string    `json:"customerId"`
	BillingType string    `json:"billingType"`
	Interval    string    `json:"interval"`
	Amount      int64     `json:"amount"`
	NextDueDate time.Time `json:"nextDueDate"`
	CreatedAt   time.Time `json:"createdAt"`
}

type adminWebhookState struct {
	ID                  string `json:"id"`
	URL                 string `json:"url"`
	Interrupted         bool   `json:"interrupted"`
	ConsecutiveFailures int    `json:"consecutiveFailures"`
	Pending             int    `json:"pending"`
}

type adminDeliveryAttempt struct {
	EventID  string    `json:"eventId"`
	Endpoint string    `json:"endpoint"`
	At       time.Time `json:"at"`
	Status   int       `json:"status"`
	Success  bool      `json:"success"`
	Err      *string   `json:"error,omitempty"`
}

type adminState struct {
	Customers     []adminCustomer        `json:"customers"`
	Charges       []adminCharge          `json:"charges"`
	Subscriptions []adminSubscription    `json:"subscriptions"`
	Webhooks      []adminWebhookState    `json:"webhooks"`
	DeliveryLog   []adminDeliveryAttempt `json:"deliveryLog"`
}

func runState(args []string) error {
	fs := flag.NewFlagSet("state", flag.ExitOnError)
	addr := fs.String("addr", defaultAddr, "address of a running localpsp serve instance")
	if err := fs.Parse(args); err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
	defer cancel()

	var st adminState
	if err := adminRequest(ctx, *addr, http.MethodGet, "/_localpsp/state", nil, &st); err != nil {
		return err
	}

	fmt.Print(formatState(st, isatty.IsTerminal(os.Stdout.Fd())))
	return nil
}

// recentDeliveries is how many of the most recent delivery log entries
// formatState prints, oldest of the shown batch first.
const recentDeliveries = 10

const (
	ansiReset = "\x1b[0m"
	ansiBold  = "\x1b[1m"
	ansiRed   = "\x1b[31m"
	ansiGreen = "\x1b[32m"
)

func paint(code, s string, color bool) string {
	if !color {
		return s
	}
	return code + s + ansiReset
}

// formatState renders a full state snapshot as a readable terminal
// report: counts, entity tables, one line per webhook queue (loud when
// interrupted) and the tail of the delivery log. color adds ANSI codes for
// status; plain output (color false) carries none.
func formatState(st adminState, color bool) string {
	var b strings.Builder

	fmt.Fprintf(&b, "localpsp state: %d customer(s), %d charge(s), %d subscription(s)\n\n",
		len(st.Customers), len(st.Charges), len(st.Subscriptions))

	writeCustomersTable(&b, st.Customers)
	b.WriteString("\n")
	writeChargesTable(&b, st.Charges, color)
	b.WriteString("\n")
	if len(st.Subscriptions) > 0 {
		writeSubscriptionsTable(&b, st.Subscriptions)
		b.WriteString("\n")
	}
	writeWebhooks(&b, st.Webhooks, color)
	b.WriteString("\n")
	writeDeliveryLog(&b, st.DeliveryLog, color)

	return b.String()
}

// table renders a header and rows as aligned, space-padded columns. Column
// widths come from the plain, uncolored cell text, and only colorize (if
// set) is applied afterward, so ANSI escape codes never throw the
// alignment off.
func table(b *strings.Builder, headers []string, rows [][]string, colorize func(col int, s string) string) {
	widths := make([]int, len(headers))
	for i, h := range headers {
		widths[i] = len(h)
	}
	for _, row := range rows {
		for i, cell := range row {
			if len(cell) > widths[i] {
				widths[i] = len(cell)
			}
		}
	}

	writeRow := func(cells []string, colored bool) {
		for i, cell := range cells {
			text := cell
			if colored && colorize != nil {
				text = colorize(i, cell)
			}
			b.WriteString(text)
			if i < len(cells)-1 {
				b.WriteString(strings.Repeat(" ", widths[i]-len(cell)))
				b.WriteString("  ")
			}
		}
		b.WriteString("\n")
	}

	writeRow(headers, false)
	for _, row := range rows {
		writeRow(row, true)
	}
}

func writeCustomersTable(b *strings.Builder, customers []adminCustomer) {
	b.WriteString("CUSTOMERS\n")
	if len(customers) == 0 {
		b.WriteString("  none yet\n")
		return
	}
	rows := make([][]string, 0, len(customers))
	for _, c := range customers {
		rows = append(rows, []string{c.ID, c.Name, c.Email})
	}
	table(b, []string{"ID", "NAME", "EMAIL"}, rows, nil)
}

func writeChargesTable(b *strings.Builder, charges []adminCharge, color bool) {
	b.WriteString("CHARGES\n")
	if len(charges) == 0 {
		b.WriteString("  none yet\n")
		return
	}
	rows := make([][]string, 0, len(charges))
	for _, c := range charges {
		rows = append(rows, []string{c.ID, c.Status, formatAmount(c.Amount), c.CustomerID})
	}
	table(b, []string{"ID", "STATUS", "AMOUNT", "CUSTOMER"}, rows, func(col int, s string) string {
		if col != 1 || !color {
			return s
		}
		code := statusColor(s)
		if code == "" {
			return s
		}
		return paint(code, s, true)
	})
}

func writeSubscriptionsTable(b *strings.Builder, subs []adminSubscription) {
	b.WriteString("SUBSCRIPTIONS\n")
	rows := make([][]string, 0, len(subs))
	for _, s := range subs {
		rows = append(rows, []string{s.ID, s.CustomerID, s.BillingType, s.Interval, formatAmount(s.Amount)})
	}
	table(b, []string{"ID", "CUSTOMER", "BILLING", "INTERVAL", "AMOUNT"}, rows, nil)
}

func statusColor(status string) string {
	switch status {
	case "confirmed", "received":
		return ansiGreen
	case "overdue":
		return ansiRed
	default:
		return ""
	}
}

func writeWebhooks(b *strings.Builder, webhooks []adminWebhookState, color bool) {
	b.WriteString("WEBHOOKS\n")
	if len(webhooks) == 0 {
		b.WriteString("  none registered\n")
		return
	}
	for _, wh := range webhooks {
		fmt.Fprintf(b, "  %s  %s\n", wh.ID, wh.URL)
		if wh.Interrupted {
			line := fmt.Sprintf("    QUEUE INTERRUPTED: %d consecutive failures, %d pending event(s)",
				wh.ConsecutiveFailures, wh.Pending)
			b.WriteString(paint(ansiBold+ansiRed, line, color))
			b.WriteString("\n")
			continue
		}
		fmt.Fprintf(b, "    queue ok, %d consecutive failure(s), %d pending event(s)\n",
			wh.ConsecutiveFailures, wh.Pending)
	}
}

func writeDeliveryLog(b *strings.Builder, log []adminDeliveryAttempt, color bool) {
	b.WriteString("RECENT DELIVERIES\n")
	if len(log) == 0 {
		b.WriteString("  none yet\n")
		return
	}

	recent := log
	if len(recent) > recentDeliveries {
		recent = recent[len(recent)-recentDeliveries:]
	}

	rows := make([][]string, 0, len(recent))
	for _, a := range recent {
		result := "success"
		if !a.Success {
			result = "failed"
		}
		rows = append(rows, []string{a.At.Local().Format("2006-01-02 15:04:05"), a.EventID, a.Endpoint, strconv.Itoa(a.Status), result})
	}
	table(b, []string{"TIME", "EVENT", "ENDPOINT", "STATUS", "RESULT"}, rows, func(col int, s string) string {
		if col != 4 || !color {
			return s
		}
		if s == "success" {
			return paint(ansiGreen, s, true)
		}
		return paint(ansiRed, s, true)
	})
}

func formatAmount(cents int64) string {
	return fmt.Sprintf("R$ %.2f", float64(cents)/100)
}
