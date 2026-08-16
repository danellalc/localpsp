package asaas

import "time"

const (
	dateLayout     = "2006-01-02"
	dateTimeLayout = "2006-01-02 15:04:05"
)

func formatDate(t time.Time) string {
	return t.UTC().Format(dateLayout)
}

func formatDateTime(t time.Time) string {
	return t.UTC().Format(dateTimeLayout)
}

func parseDate(s string) (time.Time, error) {
	return time.ParseInLocation(dateLayout, s, time.UTC)
}
