package ids

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"time"
)

// ObservationID builds a stable observation id from project, session, source, and
// a content hash so repeated observations are upserted rather than duplicated.
func ObservationID(projectID, sessionID, source, stream, text string) string {
	h := sha256.Sum256([]byte(projectID + "|" + sessionID + "|" + source + "|" + stream + "|" + text))
	return Observation + ":" + hex.EncodeToString(h[:12])
}

// LogTemplateID builds a stable id for a normalized log template.
func LogTemplateID(projectID, template string) string {
	h := sha256.Sum256([]byte(projectID + "|" + template))
	return "logtemplate:" + hex.EncodeToString(h[:12])
}

// NormalizeLogTemplate replaces digits, hex addresses, and quoted strings with
// placeholders so "target lost id=1" and "target lost id=2" share a template.
func NormalizeLogTemplate(s string) string {
	s = strings.TrimSpace(s)
	var b strings.Builder
	i := 0
	for i < len(s) {
		c := s[i]
		switch {
		case c >= '0' && c <= '9':
			b.WriteByte('%')
			for i < len(s) && s[i] >= '0' && s[i] <= '9' {
				i++
			}
			continue
		case (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F'):
			b.WriteByte('%')
			for i < len(s) {
				c2 := s[i]
				if (c2 >= '0' && c2 <= '9') || (c2 >= 'a' && c2 <= 'f') || (c2 >= 'A' && c2 <= 'F') {
					i++
					continue
				}
				break
			}
			continue
		case c == '"' || c == '\'':
			b.WriteByte('%')
			i++
			for i < len(s) && s[i] != c {
				i++
			}
			i++
			continue
		}
		b.WriteByte(c)
		i++
	}
	return b.String()
}

// TruncateBytes caps a string to max bytes, appending an ellipsis.
func TruncateBytes(s string, max int) string {
	if max <= 0 || len(s) <= max {
		return s
	}
	return s[:max] + "..."
}

// NowUTC returns the current time in RFC3339 format.
func NowUTC() string {
	return time.Now().UTC().Format(time.RFC3339)
}
