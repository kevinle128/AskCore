package protocol

import (
	"html"
	"regexp"
	"strconv"
	"strings"
)

// IsSystemReminderText reports whether text has the shape that SystemReminder
// produces. A queue update carries only texts, so a client checks the text.
func IsSystemReminderText(text string) bool {
	return len(text) >= len(systemReminderOpen)+len(systemReminderClose) &&
		strings.HasPrefix(text, systemReminderOpen) && strings.HasSuffix(text, systemReminderClose)
}

// SystemReminderBody returns the text inside the reminder of m, when m is a
// system reminder.
func SystemReminderBody(m UserMessage) (string, bool) {
	if !IsSystemReminder(m) {
		return "", false
	}
	t := m.Content[0].(Text).Text
	return t[len(systemReminderOpen) : len(t)-len(systemReminderClose)], true
}

// JobNotice is a notice of a background job, as the job table writes it: the
// completion notice, the stall notice and the notice of the background time
// limit.
type JobNotice struct {
	JobID  string
	Kind   string
	Label  string
	Status string // completed, failed, killed, or running for a stall notice
	// Detail is the short reason: "exit 2", "timeout", "signal: SIGSEGV",
	// "no output for 5 minutes", "background time limit". It is empty when the
	// notice has none.
	Detail string
}

var jobNoticeRE = regexp.MustCompile(`^Background job (\S+) \(([a-z]+): (.*)\) finished \[status: ([a-z]+)(?:, (.*))?\]\. Read its output with job_output\.$`)

// The stall and limit notices write the label with %q, so a label may hold
// a quote or a bracket; the quoted form ends the label without a guess.
const quotedLabel = `("(?:[^"\\]|\\.)*")`

var (
	jobStallRE = regexp.MustCompile(`^Background command ` + quotedLabel + ` \(job (\S+)\) printed no output for (.+?)\. It still runs\. Read it with job_output, stop it with job_kill, or leave it running\.$`)
	jobLimitRE = regexp.MustCompile(`^Background command ` + quotedLabel + ` was stopped after reaching its background time limit\. It was job (\S+)\. Read its output with job_output; start it again with a longer timeout if the work still needs it\.$`)
)

// ParseJobNotice reads the text of a completion notice, a stall notice or a
// background time limit notice.
func ParseJobNotice(text string) (JobNotice, bool) {
	text = strings.TrimSpace(text)
	if m := jobStallRE.FindStringSubmatch(text); m != nil {
		label, err := strconv.Unquote(m[1])
		if err != nil {
			return JobNotice{}, false
		}
		return JobNotice{JobID: m[2], Kind: "bash", Label: label, Status: "running", Detail: "no output for " + m[3]}, true
	}
	if m := jobLimitRE.FindStringSubmatch(text); m != nil {
		label, err := strconv.Unquote(m[1])
		if err != nil {
			return JobNotice{}, false
		}
		return JobNotice{JobID: m[2], Kind: "bash", Label: label, Status: "killed", Detail: "background time limit"}, true
	}
	m := jobNoticeRE.FindStringSubmatch(text)
	if m == nil {
		return JobNotice{}, false
	}
	detail := m[5]
	switch {
	case strings.HasPrefix(detail, "exit code: "):
		detail = "exit " + strings.TrimPrefix(detail, "exit code: ")
	case strings.HasPrefix(detail, "reason: "):
		detail = strings.TrimPrefix(detail, "reason: ")
	}
	return JobNotice{JobID: m[1], Kind: m[2], Label: m[3], Status: m[4], Detail: detail}, true
}

// MonitorEvents is one monitor-event element of a reminder: the job and the
// number of event lines it carries.
type MonitorEvents struct {
	JobID       string
	Description string
	Events      int
}

var monitorEventRE = regexp.MustCompile(`(?s)<monitor-event job="([^"]*)" description="([^"]*)">\n(.*?)\n</monitor-event>`)

// ParseMonitorEvents reads the monitor-event elements of a reminder text. Lines
// in square brackets (a cut or a flood guard note) are not events.
func ParseMonitorEvents(text string) ([]MonitorEvents, bool) {
	var out []MonitorEvents
	for _, m := range monitorEventRE.FindAllStringSubmatch(text, -1) {
		n := 0
		for _, line := range strings.Split(m[3], "\n") {
			if line != "" && (!strings.HasPrefix(line, "[") || !strings.HasSuffix(line, "]")) {
				n++
			}
		}
		out = append(out, MonitorEvents{JobID: html.UnescapeString(m[1]), Description: html.UnescapeString(m[2]), Events: n})
	}
	return out, len(out) > 0
}
