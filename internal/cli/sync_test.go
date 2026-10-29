package cli

import (
	"testing"
)

func TestDidaDateToReflowUsesTaskTimeZone(t *testing.T) {
	if got := didaDateToReflow("2026-11-01T16:00:00.000+0000", "Asia/Shanghai"); got != "2026-11-02" {
		t.Fatalf("all-day UTC midnight should read as the task-zone date, got %q", got)
	}
	if got := didaDateToReflow("2026-11-10", ""); got != "2026-11-10" {
		t.Fatalf("date-only values should pass through, got %q", got)
	}
	if got := didaDateToReflow("", ""); got != "" {
		t.Fatalf("empty dates should stay empty, got %q", got)
	}
}

func TestReflowDateToDidaIsLocalMidnight(t *testing.T) {
	value := reflowDateToDida("2026-11-02")
	if len(value) < 19 || value[:10] != "2026-11-02" || value[10] != 'T' {
		t.Fatalf("expected a local ISO timestamp, got %q", value)
	}
	if didaDateToReflow(value, "Asia/Shanghai") != "2026-11-02" {
		t.Fatalf("date should survive a Dida round trip, got %q", didaDateToReflow(value, "Asia/Shanghai"))
	}
}

func TestPlanDidaSyncSeedsFromTheOnlyNonEmptySide(t *testing.T) {
	project := &DidaProjectState{Links: map[string]*didaLink{}}
	local := []DidaSyncTask{{ID: "r-1", Title: "写摘要"}}
	result := &DidaSyncResult{}
	actions, err := planDidaSync(project, local, nil, "merge", "", result)
	if err != nil {
		t.Fatal(err)
	}
	if result.Seed != "reflow" || len(actions) != 1 || actions[0].Action != "create_dida" {
		t.Fatalf("expected a ReFlow seed, got seed=%q actions=%+v", result.Seed, actions)
	}
}

func TestPlanDidaSyncKeepsReflowOnConflict(t *testing.T) {
	oldLocal := DidaSyncTask{ID: "r-1", Title: "旧标题", DueDate: "2026-11-01"}
	oldRemote := didaTask{ID: "d-1", Title: "旧标题", DueDate: "2026-11-01T00:00:00.000+0000", TimeZone: "Asia/Shanghai"}
	local := DidaSyncTask{ID: "r-1", Title: "ReFlow 新标题", DueDate: "2026-11-02"}
	remote := didaTask{ID: "d-1", Title: "Dida 新标题", DueDate: "2026-11-03T00:00:00.000+0000", TimeZone: "Asia/Shanghai"}
	project := &DidaProjectState{Links: map[string]*didaLink{
		"r-1": {
			DidaID:     "d-1",
			ReflowHash: snapshotHash(localSnapshot(oldLocal)),
			DidaHash:   snapshotHash(remoteSnapshot(oldRemote, nil)),
		},
	}}
	actions, err := planDidaSync(project, []DidaSyncTask{local}, []didaTask{remote}, "merge", "", &DidaSyncResult{})
	if err != nil {
		t.Fatal(err)
	}
	if len(actions) != 1 || actions[0].Action != "conflict_keep_reflow" {
		t.Fatalf("expected a ReFlow-wins conflict, got %+v", actions)
	}
}

func TestPlanDidaSyncLeavesMissingRowsAsOrphans(t *testing.T) {
	project := &DidaProjectState{Links: map[string]*didaLink{
		"r-missing": {DidaID: "d-1", ReflowHash: "x", DidaHash: "y"},
	}}
	actions, err := planDidaSync(project, nil, []didaTask{{ID: "d-1", Title: "Dida only"}}, "merge", "", &DidaSyncResult{})
	if err != nil {
		t.Fatal(err)
	}
	if len(actions) != 1 || actions[0].Action != "orphan" {
		t.Fatalf("expected an orphan report, got %+v", actions)
	}
}
