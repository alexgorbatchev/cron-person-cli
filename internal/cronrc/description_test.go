package cronrc

import (
	"strings"
	"testing"
)

func TestDescribeSchedule(t *testing.T) {
	tests := []struct {
		schedule string
		want     []string
	}{
		{"*/30 * * * *", []string{"Every 30 minutes"}},
		{"*/5 * * * *", []string{"Every 5 minutes"}},
		{"*/35 * * * *", []string{"0 and 35 minutes past the hour"}},
		{"*/60 * * * *", []string{"Every hour"}},
		{"0 */23 * * *", []string{"At 00:00 and 23:00"}},
		{"0 14 * * *", []string{"At 14:00"}},
		{"0 9 * * MON-FRI", []string{"At 09:00", "Monday", "Friday"}},
		{"0 9 * * 7", []string{"Sunday"}},
		{"0 9 * * 5-7", []string{"Sunday", "Friday", "Saturday"}},
		{"0 9 * * 5-7/2", []string{"Friday"}},
		{"0 9 * * 1,3/2", []string{"Monday", "Wednesday", "Friday"}},
		{"0 9 1 * 1", []string{"day 1 of the month; or At 09:00", "Monday"}},
		{"0 9 1 JAN MON", []string{"January; or At 09:00", "Monday", "January"}},
		{"0 9 */2 * MON", []string{"every 2 days", "Monday"}},
		{"@hourly", []string{"Every hour"}},
		{"@daily", []string{"At 00:00"}},
		{"@midnight", []string{"At 00:00"}},
		{"@weekly", []string{"Sunday"}},
		{"@monthly", []string{"day 1 of the month"}},
		{"@yearly", []string{"day 1 of the month", "January"}},
		{"@annually", []string{"day 1 of the month", "January"}},
		{"@reboot", []string{"At system startup"}},
	}
	for _, tt := range tests {
		t.Run(tt.schedule, func(t *testing.T) {
			got, err := DescribeSchedule(tt.schedule)
			if err != nil {
				t.Fatal(err)
			}
			for _, want := range tt.want {
				if !strings.Contains(got, want) {
					t.Errorf("description %q does not contain %q", got, want)
				}
			}
		})
	}
}

func TestDescribeScheduleInvalid(t *testing.T) {
	for _, schedule := range []string{"", "@every", "@unknown", "60 * * * *", "0 0 * * 8"} {
		t.Run(schedule, func(t *testing.T) {
			if _, err := DescribeSchedule(schedule); err == nil {
				t.Fatal("expected an invalid schedule error")
			}
		})
	}
}
