package main

import (
	"fmt"
	"time"

	"github.com/breez/lspd/history"
	"github.com/urfave/cli"
)

var timeRangeFlags []cli.Flag = []cli.Flag{
	cli.UintFlag{
		Name:     "year",
		Required: false,
		Usage:    "Year (e.g. '2024'). Can be combined with month and day for more granularity.",
	},
	cli.UintFlag{
		Name:     "month",
		Required: false,
		Usage:    "Month (e.g. '1' for January). Can be combined with day for more granularity.",
	},
	cli.UintFlag{
		Name:     "day",
		Required: false,
		Usage:    "Day of the month (e.g. 1 for the first day of the month).",
	},
	cli.StringFlag{
		Name:     "start",
		Required: false,
		Usage:    "Start of the period. RFC3339 time, e.g. '2024-01-01T00:00:00Z' or '2024-01-01T02:00:00-02:00'",
	},
	cli.StringFlag{
		Name:     "end",
		Required: false,
		Usage:    "End of the period. RFC3339 time, e.g. '2024-02-01T00:00:00Z' or '2024-02-01T02:00:00-02:00'",
	},
}

func getTimeRange(ctx *cli.Context) (*history.TimeRange, error) {
	year := int(ctx.Uint("year"))
	hasYear := year != 0
	month := int(ctx.Uint("month"))
	hasMonth := month != 0
	day := int(ctx.Uint("day"))
	hasDay := day != 0
	start := ctx.String("start")
	hasStart := start != ""
	end := ctx.String("end")
	hasEnd := end != ""

	if (hasYear || hasMonth || hasDay) && (hasStart || hasEnd) {
		return nil, fmt.Errorf("either year/month/day or start/end can be set, but not both")
	}

	if hasMonth && (month < 1 || month > 12) {
		return nil, fmt.Errorf("month must be between 1 and 12")
	}

	if hasDay && (day < 1 || day > 31) {
		return nil, fmt.Errorf("day must be between 1 and 31")
	}

	if hasYear {
		if !hasMonth {
			month = 1
		}

		if !hasDay {
			day = 1
		}
		start := time.Date(year, time.Month(month), day, 0, 0, 0, 0, time.Local)
		if !hasMonth && !hasDay {
			return &history.TimeRange{
				Start: start,
				End:   start.AddDate(1, 0, 0),
			}, nil
		}

		if !hasMonth && hasDay {
			return nil, fmt.Errorf("year and day were supplied, but not month")
		}

		if hasMonth && !hasDay {
			return &history.TimeRange{
				Start: start,
				End:   start.AddDate(0, 1, 0),
			}, nil
		}

		return &history.TimeRange{
			Start: start,
			End:   start.AddDate(0, 0, 1),
		}, nil
	}

	// If year was set, it would have been handled above, so year is not set.
	year = time.Now().Year()
	if hasMonth {
		if !hasDay {
			day = 1
		}

		start := time.Date(year, time.Month(month), day, 0, 0, 0, 0, time.Local)
		if !hasDay {
			return &history.TimeRange{
				Start: start,
				End:   start.AddDate(0, 1, 0),
			}, nil
		}

		return &history.TimeRange{
			Start: start,
			End:   start.AddDate(0, 0, 1),
		}, nil
	}

	// If month was set, it would have been handled above, so year is not set.
	month = int(time.Now().Month())
	if hasDay {
		start := time.Date(year, time.Month(month), day, 0, 0, 0, 0, time.Local)
		return &history.TimeRange{
			Start: start,
			End:   start.AddDate(0, 0, 1),
		}, nil
	}

	// year/month/day was not set, otherwise it would have been handled above.
	// try start and end dates.
	if (hasStart && !hasEnd) || (!hasStart && hasEnd) {
		return nil, fmt.Errorf("both start and end have to be specified")
	}

	if hasStart && hasEnd {
		start, err := parseTime(start)
		if err != nil {
			return nil, fmt.Errorf("invalid start: %w", err)
		}

		end, err := parseTime(end)
		if err != nil {
			return nil, fmt.Errorf("invalid end: %w", err)
		}

		if end.Before(*start) {
			return nil, fmt.Errorf("end cannot be before start")
		}

		return &history.TimeRange{
			Start: *start,
			End:   *end,
		}, nil
	}

	// None of the fields were set, default to previous month.
	now := time.Now()
	endTime := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.Local)
	startTime := endTime.AddDate(0, -1, 0)
	return &history.TimeRange{
		Start: startTime,
		End:   endTime,
	}, nil
}

func parseTime(s string) (*time.Time, error) {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return nil, fmt.Errorf("invalid format, not an iso-8601 time': %w", err)
	}

	return &t, nil
}
