package tplink

import (
	"fmt"
	"strconv"
	"strings"
)

func normMAC(s string) string { return strings.ToLower(strings.ReplaceAll(s, "-", ":")) }

func bandOf(wireType string) Band {
	switch {
	case wireType == "wired":
		return BandWired
	case strings.HasPrefix(wireType, "2.4"):
		return Band24
	case strings.HasPrefix(wireType, "5"):
		return Band5
	case strings.HasPrefix(wireType, "6"):
		return Band6
	}
	return Band(wireType)
}

type num float64

func (n *num) UnmarshalJSON(b []byte) error {
	s := strings.Trim(string(b), `"`)
	if s == "" || s == "null" {
		return nil
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return fmt.Errorf("tplink: bad number %q: %w", s, err)
	}
	*n = num(f)
	return nil
}
