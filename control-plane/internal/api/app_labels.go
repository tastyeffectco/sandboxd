package api

import (
	"fmt"
	"regexp"
)

// App labels (ADR 0003, C7): free-form key/values the desktop uses to
// remember an app's role, e.g. env=live, source=<local app id>.

const (
	maxLabels        = 32
	maxLabelValueLen = 256
)

var reLabelKey = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,62}$`)

// labelsError returns a plain-language problem with a label set, or "".
func labelsError(l map[string]string) string {
	if len(l) > maxLabels {
		return fmt.Sprintf("an app can have at most %d labels", maxLabels)
	}
	for k, v := range l {
		if !reLabelKey.MatchString(k) {
			return fmt.Sprintf("invalid label key %q (lowercase letters, digits, '.', '_' or '-', up to 63 characters)", k)
		}
		if len(v) > maxLabelValueLen {
			return fmt.Sprintf("label %q is longer than %d characters", k, maxLabelValueLen)
		}
	}
	return ""
}

// labelsMatch reports whether have contains every key=value of want.
func labelsMatch(have, want map[string]string) bool {
	for k, v := range want {
		if got, ok := have[k]; !ok || got != v {
			return false
		}
	}
	return true
}
