package domainfinance

import (
	"encoding/json"
	"errors"
	"testing"
)

func TestParseMoneyTable(t *testing.T) {
	valid := []struct {
		in   string
		neg  bool
		want Money
	}{
		{"25000", false, 2500000},
		{"1005422.44", false, 100542244},
		{"0.01", false, 1},
		{"0.1", false, 10},
		{"0", false, 0},
		{"-0.01", true, -1},
		{"-5", true, -500},
		{"00012", false, 1200},
		{"-0", true, 0},
	}
	for _, c := range valid {
		got, err := ParseMoney(c.in, c.neg)
		if err != nil || got != c.want {
			t.Errorf("ParseMoney(%q,%v) = %d, %v; want %d", c.in, c.neg, got, err, c.want)
		}
	}
	invalid := []struct {
		in   string
		neg  bool
		want error
	}{
		{"", false, ErrMoneyFormat},
		{"1,5", false, ErrMoneyFormat},
		{"1.005.422,44", false, ErrMoneyFormat},
		{"1e3", false, ErrMoneyFormat},
		{" 5", false, ErrMoneyFormat},
		{"+5", false, ErrMoneyFormat},
		{"5.", false, ErrMoneyFormat},
		{".5", false, ErrMoneyFormat},
		{"5.123", false, ErrMoneyPrecision},
		{"abc", false, ErrMoneyFormat},
		{"-5", false, ErrMoneyFormat},
		{"-0", false, ErrMoneyFormat},
		{"\u0661\u0662", false, ErrMoneyFormat}, // Arabic-Indic digits
		{"\uff11\uff12", false, ErrMoneyFormat}, // full-width digits
		{"1..2", false, ErrMoneyFormat},
		{"0x10", false, ErrMoneyFormat},
		{"1_000", false, ErrMoneyFormat},
		{"5\n", false, ErrMoneyFormat},
		{"-", true, ErrMoneyFormat},
		{"10000000000.01", false, ErrAmountTooLarge},
		{"99999999999999999999", false, ErrAmountTooLarge},
		{"-10000000000.01", true, ErrAmountTooLarge},
	}
	for _, c := range invalid {
		got, err := ParseMoney(c.in, c.neg)
		if !errors.Is(err, c.want) || got != 0 {
			t.Errorf("ParseMoney(%q,%v) = %d, %v; want error %v", c.in, c.neg, got, err, c.want)
		}
	}
}

func TestParseMoneyBoundaryExact(t *testing.T) {
	if got, err := ParseMoney("10000000000.00", false); err != nil || got != MaxAmount {
		t.Fatalf("got %d, %v", got, err)
	}
	if got, err := ParseMoney("-10000000000", true); err != nil || got != -MaxAmount {
		t.Fatalf("got %d, %v", got, err)
	}
	if _, err := ParseMoney("10000000000.01", false); !errors.Is(err, ErrAmountTooLarge) {
		t.Fatalf("err = %v", err)
	}
}

func TestMoneyStringRoundTrip(t *testing.T) {
	cases := []struct {
		m    Money
		want string
	}{
		{0, "0"}, {1, "0.01"}, {10, "0.10"}, {100, "1"}, {2500000, "25000"},
		{100542244, "1005422.44"}, {-50, "-0.50"}, {-1, "-0.01"}, {-100, "-1"},
		{MaxAmount, "10000000000"},
	}
	for _, c := range cases {
		if got := c.m.String(); got != c.want {
			t.Errorf("Money(%d).String() = %q, want %q", c.m, got, c.want)
		}
		back, err := ParseMoney(c.want, true)
		if err != nil || back != c.m {
			t.Errorf("ParseMoney(%q) = %d, %v; want %d", c.want, back, err, c.m)
		}
	}
}

func TestMoneyJSONRoundTrip(t *testing.T) {
	marshal := []struct {
		m    Money
		want string
	}{{2500000, `"25000"`}, {100542244, `"1005422.44"`}, {50, `"0.50"`}, {-1, `"-0.01"`}, {0, `"0"`}, {-MaxAmount, `"-10000000000"`}}
	for _, c := range marshal {
		b, err := json.Marshal(c.m)
		if err != nil || string(b) != c.want {
			t.Errorf("Marshal(%d) = %s, %v; want %s", c.m, b, err, c.want)
		}
	}
	ok := []struct {
		in   string
		want Money
	}{{`1005422.44`, 100542244}, {`"1005422.44"`, 100542244}, {`25000`, 2500000}, {`-0.01`, -1}}
	for _, c := range ok {
		var m Money
		if err := json.Unmarshal([]byte(c.in), &m); err != nil || m != c.want {
			t.Errorf("Unmarshal(%s) = %d, %v; want %d", c.in, m, err, c.want)
		}
	}
	for _, in := range []string{`null`, `true`, `1e2`, `"1,5"`, `1.005`, `""`, `[]`, `{}`, `"1e2"`} {
		var m Money
		if err := json.Unmarshal([]byte(in), &m); err == nil {
			t.Errorf("Unmarshal(%s) = %d, want error", in, m)
		}
	}
	var s struct {
		A Money `json:"a"`
	}
	if err := json.Unmarshal([]byte(`{"a":12.5}`), &s); err != nil || s.A != 1250 {
		t.Errorf("struct field: %d, %v", s.A, err)
	}
}

func TestMoneyNeverUsesFloat(t *testing.T) {
	for _, in := range []string{"0.29", "1000000000.07", "0.57", "4.35", "8.2"} {
		m, err := ParseMoney(in, false)
		if err != nil {
			t.Fatal(err)
		}
		b, _ := json.Marshal(m)
		var back Money
		if err := json.Unmarshal(b, &back); err != nil || back != m {
			t.Errorf("%s: %d -> %s -> %d, %v", in, m, b, back, err)
		}
	}
	if m, _ := ParseMoney("0.29", false); m != 29 {
		t.Errorf("0.29 = %d", m)
	}
	if m, _ := ParseMoney("1000000000.07", false); m != 100000000007 {
		t.Errorf("got %d", m)
	}
}

func TestMoneyStringExtremes(t *testing.T) {
	if got := Money(-9223372036854775808).String(); got != "-92233720368547758.08" {
		t.Errorf("min int64 = %q", got)
	}
	if got := Money(9223372036854775807).String(); got != "92233720368547758.07" {
		t.Errorf("max int64 = %q", got)
	}
}
