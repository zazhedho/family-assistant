package domainfinance

import (
	"bytes"
	"errors"
	"fmt"
	"strconv"
)

// Money is an amount of rupiah (IDR) held as an exact count of sen, 1/100
// rupiah. Every conversion works on decimal text with integer arithmetic;
// floats are never involved.
type Money int64

// MaxAmount is the largest allowed amount: 1_000_000_000_000 sen, i.e.
// Rp10.000.000.000. It is also the bound of the database CHECK constraints.
const MaxAmount Money = 1_000_000_000_000

const (
	senPerRupiah = 100
	// maxWholeDigits is the most digits a whole-rupiah part can have while
	// still fitting MaxAmount (10000000000 has 11).
	maxWholeDigits = 11
)

var (
	ErrMoneyFormat    = errors.New("amount must be a plain decimal number such as 25000 or 1005422.44 (digits with an optional '.' and 1 or 2 decimals; no thousands separators, commas, spaces, '+' or exponent)")
	ErrMoneyPrecision = errors.New("amount has at most 2 decimal places, no rounding is done")
	ErrAmountTooLarge = errors.New("amount exceeds the limit of 10000000000 rupiah")
)

// ParseMoney parses strict decimal text into sen: an optional leading '-'
// (only when allowNegative), digits, and optionally '.' followed by 1 or 2
// digits. The result is bounded by -MaxAmount..MaxAmount; zero is accepted
// (ValidateAmount rejects it where an amount must be positive).
func ParseMoney(s string, allowNegative bool) (Money, error) {
	neg := false
	if allowNegative && len(s) > 0 && s[0] == '-' {
		neg = true
		s = s[1:]
	}
	whole, frac, hasDot := s, "", false
	for i := 0; i < len(s); i++ {
		if s[i] == '.' {
			whole, frac, hasDot = s[:i], s[i+1:], true
			break
		}
	}
	if whole == "" || !allDigits(whole) || (hasDot && (frac == "" || !allDigits(frac))) {
		return 0, ErrMoneyFormat
	}
	if len(frac) > 2 {
		return 0, ErrMoneyPrecision
	}
	for len(whole) > 1 && whole[0] == '0' {
		whole = whole[1:]
	}
	if len(whole) > maxWholeDigits {
		return 0, ErrAmountTooLarge
	}
	var sen int64
	for i := 0; i < len(whole); i++ {
		sen = sen*10 + int64(whole[i]-'0')
	}
	sen *= senPerRupiah
	switch len(frac) {
	case 1:
		sen += int64(frac[0]-'0') * 10
	case 2:
		sen += int64(frac[0]-'0')*10 + int64(frac[1]-'0')
	}
	if sen > int64(MaxAmount) {
		return 0, ErrAmountTooLarge
	}
	if neg {
		sen = -sen
	}
	return Money(sen), nil
}

func allDigits(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return s != ""
}

// String renders the amount without a decimal point when it is whole rupiah
// and with exactly two decimals otherwise: 25000, 1005422.44, -0.50.
func (m Money) String() string {
	// Divide first, then negate the (small) quotient and remainder, so the
	// most negative int64 cannot overflow.
	sign := ""
	whole, frac := int64(m)/senPerRupiah, int64(m)%senPerRupiah
	if m < 0 {
		whole, frac, sign = -whole, -frac, "-"
	}
	if frac == 0 {
		return sign + strconv.FormatInt(whole, 10)
	}
	return fmt.Sprintf("%s%d.%02d", sign, whole, frac)
}

// MarshalJSON renders the amount as a JSON number with the String text.
func (m Money) MarshalJSON() ([]byte, error) {
	return []byte(m.String()), nil
}

// UnmarshalJSON accepts a JSON number or a JSON string holding the decimal
// text. It parses the raw bytes, never a float64; null is rejected. A
// negative value parses here and is range-checked by the validators.
func (m *Money) UnmarshalJSON(b []byte) error {
	b = bytes.TrimSpace(b)
	if len(b) >= 2 && b[0] == '"' && b[len(b)-1] == '"' {
		b = b[1 : len(b)-1]
	}
	v, err := ParseMoney(string(b), true)
	if err != nil {
		return err
	}
	*m = v
	return nil
}
