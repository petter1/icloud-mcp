package contacts

import "testing"

// Ported from Orchard tests/swift/phones/main.swift. Synthetic numbers only.
func TestInternationalPhoneMatchesOrchardRules(t *testing.T) {
	cases := []struct {
		value, code, want, label string
	}{
		{"079 123 45 67", "41", "+41 79 123 45 67", "national CH"},
		{"0791234567", "41", "+41 79 123 45 67", "compact CH"},
		{"0041 79 123 45 67", "41", "+41 79 123 45 67", "00 prefix CH"},
		{"+41 (0)79 123 45 67", "41", "+41 79 123 45 67", "trunk marker"},
		{"+41 79 123 45 67", "41", "", "already international"},
		{"+41791234567", "41", "+41 79 123 45 67", "compact international CH grouped"},
		{"+4917600000000", "41", "+49 176 00000000", "compact international DE grouped"},
		{" +33600000000 ", "41", "+33 6 00 00 00 00", "compact international FR grouped"},
		{"+49 17600000000", "41", "", "own spacing left alone"},
		{"+49-176-00000000", "41", "", "own separators left alone"},
		{"+999123456789", "41", "", "compact unknown country left alone"},
		{"+49228000000", "41", "", "compact unclear DE shape left alone"},
		{"0049 30 1234567", "41", "+49 30 1234567", "00 prefix DE"},
		{"030 1234567", "49", "+49 30 1234567", "national DE"},
		{"0176 00000000", "49", "+49 176 00000000", "DE mobile 11 digits"},
		{"0049 151 0000000", "41", "+49 151 0000000", "DE mobile 10 digits"},
		{"089 0000000", "49", "+49 89 0000000", "DE Munich"},
		{"0228 000000", "49", "+49228000000", "DE other area code stays one block"},
		{"0176 000", "49", "+49176000", "DE mobile too short stays one block"},
		{"0664 0000000", "43", "+43 664 0000000", "AT mobile"},
		{"0043 1 0000000", "41", "+43 1 0000000", "AT Vienna"},
		{"0316 000000", "43", "+43316000000", "AT Graz stays one block"},
		{"06 00 00 00 00", "33", "+33 6 00 00 00 00", "FR mobile"},
		{"0033 1 00 00 00 00", "41", "+33 1 00 00 00 00", "FR Paris"},
		{"0033 6 00 00 00", "41", "+336000000", "FR wrong length stays one block"},
		{"0039 347 000 0000", "41", "+39 347 000 0000", "IT mobile"},
		{"0039 06 0000000", "41", "+39060000000", "IT landline stays one block"},
		{"00423 200 00 00", "41", "+423 200 00 00", "LI"},
		{"0031 6 00000000", "41", "+31 6 00000000", "NL mobile"},
		{"0044 7700 000000", "41", "+44 7700 000000", "UK mobile"},
		{"0044 20 0000 0000", "41", "+442000000000", "UK landline stays one block"},
		{"001 212 555 0100", "41", "+1 212 555 0100", "US"},
		{"0041 79 000 00", "41", "+417900000", "CH wrong length stays one block"},
		{"117", "41", "", "short code"},
		{"1234567", "41", "", "no prefix"},
		{"079 123 45 67 ext. 5", "41", "", "letters"},
	}
	for _, test := range cases {
		got, ok := InternationalPhone(test.value, test.code)
		if test.want == "" {
			if ok {
				t.Errorf("%s: InternationalPhone(%q) = %q, want unchanged", test.label, test.value, got)
			}
			continue
		}
		if !ok || got != test.want {
			t.Errorf("%s: InternationalPhone(%q) = %q, %v; want %q", test.label, test.value, got, ok, test.want)
		}
	}
}

func TestFormatPhoneAndKey(t *testing.T) {
	if got := FormatPhone("4917600000000"); got != "+49 176 00000000" {
		t.Errorf("format DE = %q", got)
	}
	if got := FormatPhone("999123456"); got != "+999123456" {
		t.Errorf("format unknown country = %q", got)
	}
	if PhoneKey("079 123 45 67") != PhoneKey("+41 79 123 45 67") {
		t.Error("key national vs international differ")
	}
	for value, want := range map[string]string{
		"0041 79 123 45 67":   "791234567",
		"+41 (0)79 123 45 67": "791234567",
		"117":                 "117",
		"12":                  "",
	} {
		if got := PhoneKey(value); got != want {
			t.Errorf("PhoneKey(%q) = %q, want %q", value, got, want)
		}
	}
}
