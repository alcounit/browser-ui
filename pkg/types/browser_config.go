package types

const SessionTypeUnknown = "unknown"

const VNCAnnotationKey = "selenosis.io/session.vnc"

const TestNameAnnotationKey = "selenosis.io/testName"

var knownSessionTypes = map[string]struct{}{
	"selenium":   {},
	"playwright": {},
	"mcp":        {},
	"devtools":   {},
}

type BrowserGroup map[string][]string

type BrowserVersions map[string]BrowserGroup

func NormalizeSessionType(sessionType string) string {
	if _, ok := knownSessionTypes[sessionType]; ok {
		return sessionType
	}
	return SessionTypeUnknown
}
