package streak

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"time"
)

// IST is the zone that defines day boundaries. India has no DST, so a fixed
// offset is exact and avoids shipping tzdata in the binary.
var IST = time.FixedZone("Asia/Kolkata", 5*60*60+30*60)

// Day is a calendar date with no time or zone, as days since 1970-01-01.
type Day int32

const dayLayout = "2006-01-02"

// DayOf returns the IST calendar date of an instant.
func DayOf(t time.Time) Day {
	y, m, d := t.In(IST).Date()
	return dayFromDate(y, m, d)
}

// ParseDay parses a YYYY-MM-DD date.
func ParseDay(s string) (Day, error) {
	t, err := time.Parse(dayLayout, s)
	if err != nil {
		return 0, fmt.Errorf("invalid date %q: want YYYY-MM-DD", s)
	}
	return dayFromDate(t.Date()), nil
}

func dayFromDate(y int, m time.Month, d int) Day {
	return Day(time.Date(y, m, d, 0, 0, 0, 0, time.UTC).Unix() / 86400)
}

func (d Day) String() string {
	return time.Unix(int64(d)*86400, 0).UTC().Format(dayLayout)
}

// Weekday returns 0 for Sunday through 6 for Saturday.
func (d Day) Weekday() int {
	return ((int(d)+4)%7 + 7) % 7 // 1970-01-01 was a Thursday.
}

func (d Day) MarshalJSON() ([]byte, error) { return json.Marshal(d.String()) }

func (d *Day) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return err
	}
	parsed, err := ParseDay(s)
	if err != nil {
		return err
	}
	*d = parsed
	return nil
}

// Value sends the day as text; queries cast the parameter with $n::date.
func (d Day) Value() (driver.Value, error) { return d.String(), nil }

// Scan reads a DATE column.
func (d *Day) Scan(src any) error {
	switch v := src.(type) {
	case time.Time:
		*d = dayFromDate(v.Date())
		return nil
	case string:
		parsed, err := ParseDay(v)
		if err != nil {
			return err
		}
		*d = parsed
		return nil
	case []byte:
		return d.Scan(string(v))
	}
	return fmt.Errorf("cannot scan %T into Day", src)
}
