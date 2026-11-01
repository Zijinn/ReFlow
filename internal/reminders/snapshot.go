package reminders

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"regexp"
	"sort"
	"strings"
	"time"
)

const markerPrefix = "Reflow-ID: "

var markerPattern = regexp.MustCompile(`(?m)^[ \t]*Reflow-ID:[ \t]*([^\s]+)[ \t]*$`)

type snapshot struct {
	Title     string `json:"title"`
	Notes     string `json:"notes"`
	DueDate   string `json:"due_date"`
	Completed bool   `json:"completed"`
}

func localSnapshot(task Task) snapshot {
	return snapshot{
		Title:     strings.TrimSpace(task.Title),
		Notes:     strings.TrimSpace(task.Notes),
		DueDate:   strings.TrimSpace(task.DueDate),
		Completed: task.Completed,
	}
}

func remoteSnapshot(reminder Reminder) snapshot {
	notes, _ := splitMarker(reminder.Notes)
	return snapshot{
		Title:     strings.TrimSpace(reminder.Name),
		Notes:     strings.TrimSpace(notes),
		DueDate:   strings.TrimSpace(reminder.DueDate),
		Completed: reminder.Completed,
	}
}

func snapshotHash(value snapshot) string {
	body, _ := json.Marshal(value)
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:])
}

func sameSnapshot(left, right snapshot) bool {
	return snapshotHash(left) == snapshotHash(right)
}

// splitMarker removes the last Reflow-ID line and returns the visible notes.
func splitMarker(notes string) (string, string) {
	matches := markerPattern.FindAllStringSubmatchIndex(notes, -1)
	if len(matches) == 0 {
		return strings.TrimSpace(notes), ""
	}
	last := matches[len(matches)-1]
	id := notes[last[2]:last[3]]
	clean := notes[:last[0]] + notes[last[1]:]
	return strings.TrimSpace(clean), id
}

// withMarker writes exactly one Reflow-ID line at the end of the notes.
func withMarker(notes, reflowID string) string {
	clean, _ := splitMarker(notes)
	marker := markerPrefix + strings.TrimSpace(reflowID)
	if clean == "" {
		return marker
	}
	return clean + "\n\n" + marker
}

// reminderDateToLocal converts the ISO instant returned by JXA into the local
// calendar date Apple Reminders shows. All-day dates arrive as the previous
// UTC day (e.g. 16:00Z for Asia/Shanghai), so taking the first ten characters
// would shift the task one day earlier.
func reminderDateToLocal(value string) string {
	return reminderDateToLocalIn(value, time.Local)
}

func reminderDateToLocalIn(value string, location *time.Location) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		if len(value) >= 10 {
			return value[:10]
		}
		return value
	}
	return parsed.In(location).Format("2006-01-02")
}

func sortedTasks(tasks []Task) []Task {
	out := append([]Task(nil), tasks...)
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Position != out[j].Position {
			return out[i].Position < out[j].Position
		}
		return out[i].ID < out[j].ID
	})
	return out
}

func sortedReminders(items []Reminder) []Reminder {
	out := append([]Reminder(nil), items...)
	sort.SliceStable(out, func(i, j int) bool {
		if (out[i].DueDate == "") != (out[j].DueDate == "") {
			return out[i].DueDate != ""
		}
		if out[i].DueDate != out[j].DueDate {
			return out[i].DueDate < out[j].DueDate
		}
		return out[i].ID < out[j].ID
	})
	return out
}

func sortedLinkIDs(links map[string]*Link) []string {
	out := make([]string, 0, len(links))
	for id := range links {
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
