package contacts

import "strings"

// Phone number rules shared with Orchard (swift/Sources/AppleBridge/PhoneNumbers.swift),
// so both backends rewrite the same numbers the same way.

func asciiDigits(value string) string {
	var digits strings.Builder
	for _, r := range value {
		if r >= '0' && r <= '9' {
			digits.WriteRune(r)
		}
	}
	return digits.String()
}

// PhoneKey is the comparison key of a phone number: the last nine digits, so
// national (079 ...) and international (+41 79 ..., 0041 79 ...) forms meet.
// Shorter numbers (short codes) must match in full. It returns "" for fewer
// than three digits.
func PhoneKey(value string) string {
	digits := asciiDigits(strings.ReplaceAll(value, "(0)", ""))
	if len(digits) < 3 {
		return ""
	}
	if len(digits) > 9 {
		return digits[len(digits)-9:]
	}
	return digits
}

// InternationalPhone returns the international form of value and true, or
// false when value already is one or its shape is unclear. "0..." gets the
// country code, "00..." becomes "+...", "+41 (0)79 ..." loses the "(0)", and
// a bare "+41791234567" block is grouped when FormatPhone knows its shape.
// Numbers without a leading 0 or + (short codes, local numbers without area
// code) and numbers with the user's own spacing after "+" stay as they are.
func InternationalPhone(value, countryCode string) (string, bool) {
	trimmed := strings.TrimSpace(value)
	hadTrunkMarker := strings.Contains(trimmed, "(0)")
	cleaned := strings.ReplaceAll(trimmed, "(0)", "")
	for _, r := range cleaned {
		if (r < '0' || r > '9') && !strings.ContainsRune(" -./() +", r) {
			return "", false
		}
	}
	digits := asciiDigits(cleaned)
	code := asciiDigits(countryCode)
	if code == "" || len(digits) < 6 {
		return "", false
	}
	var full string
	switch {
	case strings.HasPrefix(cleaned, "+"):
		if !hadTrunkMarker && asciiDigits(cleaned[1:]) != cleaned[1:] {
			return "", false
		}
		full = digits
	case strings.HasPrefix(digits, "00"):
		full = digits[2:]
	case strings.HasPrefix(digits, "0"):
		full = code + digits[1:]
	default:
		return "", false
	}
	formatted := FormatPhone(full)
	if formatted == trimmed {
		return "", false
	}
	return formatted, true
}

type phonePlan struct {
	code   string
	groups func(national string) []int
}

// phonePlans lists the national numbering plans with an unambiguous shape.
// Country codes are prefix-free, so at most one matches.
var phonePlans = []phonePlan{
	{"41", func(n string) []int { return fixedGroups(n, 9, 2, 3, 2, 2) }},
	{"423", func(n string) []int { return fixedGroups(n, 7, 3, 2, 2) }},
	{"49", func(n string) []int {
		switch {
		case hasAnyPrefix(n, "15", "16", "17") && len(n) >= 10 && len(n) <= 11:
			return []int{3, len(n) - 3}
		case hasAnyPrefix(n, "30", "40", "69", "89") && len(n) >= 7 && len(n) <= 10:
			return []int{2, len(n) - 2}
		}
		return nil
	}},
	{"43", func(n string) []int {
		switch {
		case strings.HasPrefix(n, "6") && len(n) >= 10 && len(n) <= 13:
			return []int{3, len(n) - 3}
		case strings.HasPrefix(n, "1") && len(n) >= 5 && len(n) <= 11:
			return []int{1, len(n) - 1}
		}
		return nil
	}},
	{"33", func(n string) []int {
		if strings.HasPrefix(n, "0") {
			return nil
		}
		return fixedGroups(n, 9, 1, 2, 2, 2, 2)
	}},
	{"39", func(n string) []int {
		if !strings.HasPrefix(n, "3") {
			return nil
		}
		return fixedGroups(n, 10, 3, 3, 4)
	}},
	{"31", func(n string) []int {
		if !strings.HasPrefix(n, "6") {
			return nil
		}
		return fixedGroups(n, 9, 1, 8)
	}},
	{"44", func(n string) []int {
		if !strings.HasPrefix(n, "7") {
			return nil
		}
		return fixedGroups(n, 10, 4, 6)
	}},
	{"1", func(n string) []int {
		if hasAnyPrefix(n, "0", "1") {
			return nil
		}
		return fixedGroups(n, 10, 3, 3, 4)
	}},
}

func fixedGroups(national string, length int, sizes ...int) []int {
	if len(national) != length {
		return nil
	}
	return sizes
}

func hasAnyPrefix(value string, prefixes ...string) bool {
	for _, prefix := range prefixes {
		if strings.HasPrefix(value, prefix) {
			return true
		}
	}
	return false
}

// FormatPhone groups "+" and the digits (country code first) by the national
// numbering plan where the shape is unambiguous (CH, LI, DE mobile and big
// cities, AT mobile and Vienna, FR, IT mobile, NL and UK mobile, US/CA),
// otherwise it returns "+" and the digits as one block. Only spaces are added;
// the digits never change.
func FormatPhone(digitsWithCountry string) string {
	for _, plan := range phonePlans {
		if !strings.HasPrefix(digitsWithCountry, plan.code) {
			continue
		}
		national := digitsWithCountry[len(plan.code):]
		sizes := plan.groups(national)
		total := 0
		for _, size := range sizes {
			total += size
		}
		if sizes == nil || total != len(national) {
			break
		}
		parts := []string{"+" + plan.code}
		for _, size := range sizes {
			parts = append(parts, national[:size])
			national = national[size:]
		}
		return strings.Join(parts, " ")
	}
	return "+" + digitsWithCountry
}
