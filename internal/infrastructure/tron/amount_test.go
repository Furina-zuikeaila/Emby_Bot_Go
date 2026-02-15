package tron

import "testing"

func TestParseDecimalToQuant(t *testing.T) {
	tests := []struct {
		in       string
		decimals int
		want     string
		ok       bool
	}{
		{"1", 6, "1000000", true},
		{"1.23", 6, "1230000", true},
		{"0.000001", 6, "1", true},
		{"0.0000010", 6, "1", true},
		{"0.0000011", 6, "", false},
		{"10.5", 6, "10500000", true},
		{"0010.50", 6, "10500000", true},
		{"0", 6, "0", true},
		{"", 6, "", false},
		{"-1", 6, "", false},
		{"1.2.3", 6, "", false},
		{"abc", 6, "", false},
		{"1.1", 0, "", false},
		{"1", 0, "1", true},
	}
	for _, tt := range tests {
		got, err := ParseDecimalToQuant(tt.in, tt.decimals)
		if tt.ok && err != nil {
			t.Fatalf("ParseDecimalToQuant(%q,%d) unexpected err: %v", tt.in, tt.decimals, err)
		}
		if !tt.ok && err == nil {
			t.Fatalf("ParseDecimalToQuant(%q,%d) expected err", tt.in, tt.decimals)
		}
		if tt.ok && got.String() != tt.want {
			t.Fatalf("ParseDecimalToQuant(%q,%d)=%s want=%s", tt.in, tt.decimals, got.String(), tt.want)
		}
	}
}
