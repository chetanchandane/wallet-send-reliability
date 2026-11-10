package money

import (
	"errors"
	"strings"
)

// MaxUnits is the largest integer represented exactly by Redis Lua numbers.
const MaxUnits int64 = 1<<53 - 1

var ErrAmount = errors.New("invalid USDC amount")

// ParseUSDC parses a positive decimal amount without rounding.
func ParseUSDC(s string) (int64, error) {
	whole, fraction, dot := strings.Cut(s, ".")
	if whole == "" || (dot && (fraction == "" || len(fraction) > 6)) {
		return 0, ErrAmount
	}
	digits := whole + fraction
	var units int64
	for _, c := range digits {
		if c < '0' || c > '9' {
			return 0, ErrAmount
		}
		d := int64(c - '0')
		if units > (MaxUnits-d)/10 {
			return 0, ErrAmount
		}
		units = units*10 + d
	}
	for i := len(fraction); i < 6; i++ {
		if units > MaxUnits/10 {
			return 0, ErrAmount
		}
		units *= 10
	}
	if units == 0 {
		return 0, ErrAmount
	}
	return units, nil
}
