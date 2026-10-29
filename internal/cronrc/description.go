package cronrc

import (
	"fmt"
	"strconv"
	"strings"

	descriptor "github.com/lnquy/cron"
	"github.com/robfig/cron/v3"
)

// DescribeSchedule describes a supported system-crontab schedule in English.
func DescribeSchedule(schedule string) (string, error) {
	if schedule == "@reboot" {
		return "At system startup", nil
	}
	aliases := map[string]string{
		"@yearly": "0 0 1 1 *", "@annually": "0 0 1 1 *",
		"@monthly": "0 0 1 * *", "@weekly": "0 0 * * 0",
		"@daily": "0 0 * * *", "@midnight": "0 0 * * *",
		"@hourly": "0 * * * *",
	}
	if expanded, ok := aliases[schedule]; ok {
		schedule = expanded
	}
	fields := strings.Fields(schedule)
	if len(fields) != 5 {
		return "", fmt.Errorf("expected a five-field schedule or supported alias, got %q", schedule)
	}
	dayOr := !strings.ContainsAny(fields[2], "*?") && !strings.ContainsAny(fields[4], "*?")
	fields[4] = normalizeDowField(fields[4])
	parsed, err := cronParser.Parse(strings.Join(fields, " "))
	if err != nil {
		return "", fmt.Errorf("parsing schedule for description: %w", err)
	}
	spec := parsed.(*cron.SpecSchedule)
	fields = descriptionFields(fields, spec)
	desc, err := descriptor.NewDescriptor(descriptor.Use24HourTimeFormat(true))
	if err != nil {
		return "", fmt.Errorf("initializing schedule descriptor: %w", err)
	}
	if dayOr {
		// System cron matches either restricted day field. Describing them
		// together would incorrectly imply that both must match.
		dow := fields[4]
		fields[4] = "*"
		byMonthDay, err := desc.ToDescription(strings.Join(fields, " "), descriptor.Locale_en)
		if err != nil {
			return "", err
		}
		fields[2], fields[4] = "*", dow
		byWeekDay, err := desc.ToDescription(strings.Join(fields, " "), descriptor.Locale_en)
		if err != nil {
			return "", err
		}
		return byMonthDay + "; or " + byWeekDay, nil
	}
	return desc.ToDescription(strings.Join(fields, " "), descriptor.Locale_en)
}

func descriptionFields(fields []string, spec *cron.SpecSchedule) []string {
	masks := []uint64{spec.Minute, spec.Hour, spec.Dom, spec.Month, spec.Dow}
	periods := []int{60, 24, 31, 12, 7}
	for i, field := range fields {
		// Keep simple intervals compact. Resolve other expressions through the
		// existing parser so lists of steps, names, and Sunday ranges retain
		// their system-crontab meaning in the descriptor's different dialect.
		if field == "*" || field == "?" {
			continue
		}
		if strings.HasPrefix(field, "*/") {
			step, err := strconv.Atoi(strings.TrimPrefix(field, "*/"))
			// Minute/hour steps reset each hour/day. An uneven division is
			// a list of times, rather than a continuous interval.
			if err == nil && step > 0 && step < periods[i] && (i > 1 || periods[i]%step == 0) {
				continue
			}
		}
		var values []string
		for value := range 60 {
			if masks[i]&(uint64(1)<<value) != 0 {
				values = append(values, strconv.Itoa(value))
			}
		}
		fields[i] = strings.Join(values, ",")
	}
	return fields
}
