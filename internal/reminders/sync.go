package reminders

import (
	"context"
	"fmt"
	"strings"
	"time"
)

type localUpdate struct {
	ID   string
	Task Task
}

type localCreate struct {
	Reminder Reminder
	Task     Task
}

type adoption struct {
	ReflowID   string
	ReminderID string
}

type plan struct {
	remoteOps       []RemoteOperation
	localUpdates    []localUpdate
	localCreates    []localCreate
	adoptions       []adoption
	actions         []Action
	warnings        []Action
	createdInRemind int
	createdInReflow int
	updatedInRemind int
	updatedInReflow int
	matched         map[string]bool
}

func (p *plan) addAction(action Action) { p.actions = append(p.actions, action) }

func (p *plan) addWarning(action Action) {
	p.warnings = append(p.warnings, action)
	p.actions = append(p.actions, action)
}

func QueryStatus(ctx context.Context, remote RemoteStore, listName, statePath string) (Status, error) {
	listName = strings.TrimSpace(listName)
	if listName == "" {
		listName = "Reflow"
	}
	status := Status{ListName: listName}
	if ok, reason := Available(); !ok {
		status.Reason = reason
		return status, nil
	}
	if strings.TrimSpace(statePath) == "" {
		path, err := DefaultStatePath()
		if err == nil {
			statePath = path
		}
	}
	var state State
	if statePath != "" {
		state, _ = loadState(statePath)
	}
	listState := state.Lists[listName]
	listID := ""
	if listState != nil {
		listID = listState.ListID
		status.LastSync = listState.LastSync
	}
	remoteStatus, err := remote.Status(ctx, listName, listID)
	if err != nil {
		var permission *PermissionError
		if asPermission(err, &permission) {
			status.PermissionRequired = true
			status.Reason = permission.Reason
		} else {
			status.Reason = err.Error()
		}
		return status, nil
	}
	status.Available = true
	status.ListExists = remoteStatus.Exists
	status.ListID = remoteStatus.List.ID
	status.ReminderCount = remoteStatus.ReminderCount
	if listState != nil && remoteStatus.List.ID != "" && listState.ListID == remoteStatus.List.ID {
		status.LinkedTasks = len(listState.Links)
	}
	return status, nil
}

func Sync(ctx context.Context, local LocalStore, remote RemoteStore, opts Options) (Result, error) {
	direction := strings.ToLower(strings.TrimSpace(opts.Direction))
	if direction == "" {
		direction = "merge"
	}
	switch direction {
	case "merge", "push", "pull":
	default:
		return Result{}, usage("Reminders sync direction must be merge, push, or pull, got %q", direction)
	}
	listName := strings.TrimSpace(opts.ListName)
	if listName == "" {
		return Result{}, usage("the Reminders list name cannot be empty")
	}
	statePath := strings.TrimSpace(opts.StatePath)
	if statePath == "" {
		path, err := DefaultStatePath()
		if err != nil {
			return Result{}, err
		}
		statePath = path
	}
	state, err := loadState(statePath)
	if err != nil {
		return Result{}, err
	}
	listState := state.Lists[listName]
	if listState == nil {
		listState = &ListState{ListName: listName, Links: map[string]*Link{}}
	}
	if listState.Links == nil {
		listState.Links = map[string]*Link{}
	}

	remoteSnap, err := remote.Snapshot(ctx, listName, listState.ListID)
	if err != nil {
		return Result{}, err
	}
	localTasks, err := local.ListTasks(ctx)
	if err != nil {
		return Result{}, err
	}

	if !remoteSnap.Exists {
		listState.Links = map[string]*Link{}
		listState.ListID = ""
	} else if remoteSnap.List.ID != "" {
		if listState.ListID != "" && listState.ListID != remoteSnap.List.ID {
			// The user deleted and recreated the list: the old reminder ids are gone.
			listState.Links = map[string]*Link{}
		}
		listState.ListID = remoteSnap.List.ID
		listState.ListName = remoteSnap.List.Name
	}

	currentPlan := buildPlan(listState, localTasks, remoteSnap.Reminders, direction)
	result := Result{
		Direction: direction, DryRun: opts.DryRun, StateFile: statePath,
		List: listFromSnapshot(remoteSnap, listName),
	}
	result.Actions = currentPlan.actions
	result.Conflicts = actionsOf(currentPlan.actions, "conflict_keep_reflow")
	result.Orphans = actionsOf(currentPlan.actions, "orphan")
	result.Skipped = actionsOf(currentPlan.actions, "skip_due_clear")
	if opts.DryRun {
		result.CreatedInReminders = currentPlan.createdInRemind
		result.CreatedInReflow = currentPlan.createdInReflow
		result.UpdatedInReminders = currentPlan.updatedInRemind
		result.UpdatedInReflow = currentPlan.updatedInReflow
		return result, nil
	}

	for _, update := range currentPlan.localUpdates {
		if err := local.UpdateTask(ctx, update.ID, update.Task); err != nil {
			return Result{}, err
		}
		result.UpdatedInReflow++
	}
	createdLocal := map[string]string{} // reminder id -> new ReFlow id
	for _, create := range currentPlan.localCreates {
		created, err := local.CreateTask(ctx, create.Task)
		if err != nil {
			return Result{}, err
		}
		createdLocal[create.Reminder.ID] = created.ID
		result.CreatedInReflow++
		notes := withMarker(create.Reminder.Notes, created.ID)
		currentPlan.remoteOps = append(currentPlan.remoteOps, RemoteOperation{
			Kind: "update", ReminderID: create.Reminder.ID, Name: firstNonEmpty(create.Reminder.Name, created.Title),
			Notes: notes, DueDate: create.Reminder.DueDate, Completed: create.Reminder.Completed,
		})
	}

	remoteAfter := remoteSnap.Reminders
	applyList := remoteSnap.List
	if len(currentPlan.remoteOps) > 0 {
		applyResult, err := remote.Apply(ctx, listName, listState.ListID, currentPlan.remoteOps)
		if err != nil {
			return Result{}, err
		}
		if failure := firstRemoteFailure(applyResult.Results); failure != nil {
			return Result{}, &TransportError{Reason: fmt.Sprintf(
				"Reminders operation %s failed: %s", failure.Kind, failure.Error)}
		}
		for _, item := range applyResult.Results {
			if item.Kind != "create" || !item.OK || item.Key == "" {
				continue
			}
			// Reminders does not return a batch's new rows in insertion order, so the
			// id read immediately after push can point at a different reminder. The
			// marker written into the notes is the authoritative correlation.
			reminderID := reminderIDByMarker(applyResult.Reminders, item.Key)
			if reminderID == "" {
				reminderID = item.ReminderID
			}
			if reminderID != "" {
				currentPlan.adoptions = append(currentPlan.adoptions, adoption{ReflowID: item.Key, ReminderID: reminderID})
			}
		}
		remoteAfter = applyResult.Reminders
		applyList = applyResult.List
	}
	result.UpdatedInReminders = currentPlan.updatedInRemind
	result.CreatedInReminders = currentPlan.createdInRemind

	localAfter, err := local.ListTasks(ctx)
	if err != nil {
		return Result{}, err
	}
	finalLinks := reconcileLinks(listState, localAfter, remoteAfter, currentPlan, createdLocal)
	listState.Links = finalLinks
	listState.ListID = applyList.ID
	if listState.ListID == "" {
		listState.ListID = remoteSnap.List.ID
	}
	if applyList.Name != "" {
		listState.ListName = applyList.Name
	}
	listState.LastSync = time.Now().UTC().Format(time.RFC3339)
	state.Lists[listName] = listState
	if err := saveState(statePath, state); err != nil {
		return Result{}, err
	}
	result.List = listFromSnapshot(RemoteSnapshot{List: applyList, Exists: applyList.ID != ""}, listName)
	return result, nil
}

func listFromSnapshot(snapshot RemoteSnapshot, fallbackName string) List {
	if snapshot.List.ID != "" || snapshot.List.Name != "" {
		return snapshot.List
	}
	return List{Name: fallbackName}
}

func reminderIDByMarker(items []Reminder, reflowID string) string {
	bestID := ""
	bestModified := ""
	for _, item := range items {
		_, markerID := splitMarker(item.Notes)
		if markerID != reflowID {
			continue
		}
		if bestID == "" || item.ModifiedAt > bestModified {
			bestID = item.ID
			bestModified = item.ModifiedAt
		}
	}
	return bestID
}

func firstRemoteFailure(results []RemoteOperationResult) *RemoteOperationResult {
	for index := range results {
		if !results[index].OK {
			return &results[index]
		}
	}
	return nil
}

func actionsOf(actions []Action, kind string) []Action {
	var out []Action
	for _, action := range actions {
		if action.Action == kind {
			out = append(out, action)
		}
	}
	return out
}

func reconcileLinks(state *ListState, local []Task, remote []Reminder, currentPlan *plan, createdLocal map[string]string) map[string]*Link {
	localByID := make(map[string]Task, len(local))
	for _, task := range local {
		localByID[task.ID] = task
	}
	remoteByID := make(map[string]Reminder, len(remote))
	for _, reminder := range remote {
		remoteByID[reminder.ID] = reminder
	}
	links := map[string]*Link{}
	for reflowID, link := range state.Links {
		if link == nil {
			continue
		}
		localTask, hasLocal := localByID[reflowID]
		remoteTask, hasRemote := remoteByID[link.ReminderID]
		if !hasLocal && !hasRemote {
			continue
		}
		copied := *link
		if hasLocal && hasRemote {
			localHash := snapshotHash(localSnapshot(localTask))
			remoteHash := snapshotHash(remoteSnapshot(remoteTask))
			copied.ReflowHash = localHash
			copied.ReminderHash = remoteHash
		}
		links[reflowID] = &copied
	}
	for _, adopted := range currentPlan.adoptions {
		localTask, hasLocal := localByID[adopted.ReflowID]
		remoteTask, hasRemote := remoteByID[adopted.ReminderID]
		if !hasLocal || !hasRemote {
			continue
		}
		links[adopted.ReflowID] = &Link{
			ReminderID:   adopted.ReminderID,
			ReflowHash:   snapshotHash(localSnapshot(localTask)),
			ReminderHash: snapshotHash(remoteSnapshot(remoteTask)),
		}
	}
	for reminderID, reflowID := range createdLocal {
		localTask, hasLocal := localByID[reflowID]
		remoteTask, hasRemote := remoteByID[reminderID]
		if !hasLocal || !hasRemote {
			continue
		}
		links[reflowID] = &Link{
			ReminderID:   reminderID,
			ReflowHash:   snapshotHash(localSnapshot(localTask)),
			ReminderHash: snapshotHash(remoteSnapshot(remoteTask)),
		}
	}
	return links
}

func buildPlan(state *ListState, local []Task, remote []Reminder, direction string) *plan {
	result := &plan{matched: map[string]bool{}}
	localByID := make(map[string]Task, len(local))
	for _, task := range local {
		localByID[task.ID] = task
	}
	remoteByID := make(map[string]Reminder, len(remote))
	remoteByMarker := map[string][]Reminder{}
	for _, reminder := range remote {
		remoteByID[reminder.ID] = reminder
		_, markerID := splitMarker(reminder.Notes)
		if markerID != "" {
			remoteByMarker[markerID] = append(remoteByMarker[markerID], reminder)
		}
	}

	linkedReflow := make(map[string]bool, len(state.Links))
	for _, reflowID := range sortedLinkIDs(state.Links) {
		link := state.Links[reflowID]
		if link == nil {
			continue
		}
		linkedReflow[reflowID] = true
		localTask, hasLocal := localByID[reflowID]
		remoteTask, hasRemote := remoteByID[link.ReminderID]
		if !hasLocal && !hasRemote {
			delete(state.Links, reflowID)
			continue
		}
		if !hasLocal || !hasRemote {
			reason := "ReFlow task is missing; Reminders copy left untouched"
			if !hasRemote {
				reason = "Reminders task is missing; ReFlow copy left untouched"
			}
			result.addAction(Action{
				Action: "orphan", ReflowID: reflowID, ReminderID: link.ReminderID,
				Title: firstNonEmpty(localTask.Title, remoteTask.Name), Reason: reason,
			})
			result.matched[link.ReminderID] = true
			continue
		}
		result.matched[link.ReminderID] = true
		handlePair(result, localTask, remoteTask, link, direction)
	}

	// A lost state file is recoverable because every reminder ReFlow creates
	// carries its task id. Match those pairs before creating new copies.
	for _, task := range sortedTasks(local) {
		if linkedReflow[task.ID] {
			continue
		}
		matches := remoteByMarker[task.ID]
		if len(matches) == 0 {
			continue
		}
		sortRemindersForMatch(matches)
		chosen := matches[0]
		for _, duplicate := range matches[1:] {
			result.matched[duplicate.ID] = true
			result.addWarning(Action{
				Action: "skip_due_clear", ReflowID: task.ID, ReminderID: duplicate.ID,
				Title: task.Title, Reason: "duplicate reminder carries the same ReFlow id; left untouched",
			})
		}
		result.matched[chosen.ID] = true
		handlePair(result, task, chosen, &Link{ReminderID: chosen.ID}, direction)
		result.adoptions = append(result.adoptions, adoption{ReflowID: task.ID, ReminderID: chosen.ID})
	}

	if direction != "pull" {
		for _, task := range sortedTasks(local) {
			if linkedReflow[task.ID] {
				continue
			}
			if len(remoteByMarker[task.ID]) > 0 {
				continue
			}
			result.createdInRemind++
			result.addAction(Action{Action: "create_reminder", ReflowID: task.ID, Title: task.Title})
			result.remoteOps = append(result.remoteOps, RemoteOperation{
				Kind: "create", Key: task.ID, Name: firstNonEmpty(task.Title, "ReFlow 任务"),
				Notes: withMarker(task.Notes, task.ID), DueDate: task.DueDate, Completed: task.Completed,
			})
		}
	}

	if direction != "push" {
		for _, reminder := range sortedReminders(remote) {
			if result.matched[reminder.ID] {
				continue
			}
			cleanNotes, markerID := splitMarker(reminder.Notes)
			if markerID != "" {
				if _, exists := localByID[markerID]; exists {
					// Already handled by the marker pass above.
					continue
				}
			}
			result.createdInReflow++
			result.addAction(Action{Action: "create_reflow", ReminderID: reminder.ID,
				Title: firstNonEmpty(reminder.Name, "未命名任务")})
			result.localCreates = append(result.localCreates, localCreate{
				Reminder: reminder,
				Task: Task{
					Title: firstNonEmpty(reminder.Name, "未命名任务"), Notes: cleanNotes,
					DueDate: reminder.DueDate, Completed: reminder.Completed,
				},
			})
		}
	}
	return result
}

func handlePair(result *plan, localTask Task, remoteTask Reminder, link *Link, direction string) {
	localSnap := localSnapshot(localTask)
	remoteSnap := remoteSnapshot(remoteTask)
	localHash := snapshotHash(localSnap)
	remoteHash := snapshotHash(remoteSnap)
	localChanged := link.ReflowHash == "" || localHash != link.ReflowHash
	remoteChanged := link.ReminderHash == "" || remoteHash != link.ReminderHash

	updateRemote := false
	updateLocal := false
	conflict := false
	switch direction {
	case "push":
		updateRemote = localHash != remoteHash
	case "pull":
		updateLocal = localHash != remoteHash
	default:
		switch {
		case !localChanged && !remoteChanged:
			return
		case localChanged && !remoteChanged:
			updateRemote = true
		case !localChanged && remoteChanged:
			updateLocal = true
		case localHash == remoteHash:
			return
		default:
			updateRemote = true
			conflict = true
		}
	}
	if updateLocal {
		result.updatedInReflow++
		result.addAction(Action{Action: "update_reflow", ReflowID: localTask.ID,
			ReminderID: remoteTask.ID, Title: firstNonEmpty(remoteTask.Name, localTask.Title)})
		result.localUpdates = append(result.localUpdates, localUpdate{
			ID: localTask.ID,
			Task: Task{
				Title: firstNonEmpty(remoteTask.Name, localTask.Title),
				Notes: mustCleanNotes(remoteTask.Notes), DueDate: remoteTask.DueDate,
				Completed: remoteTask.Completed,
			},
		})
		return
	}
	if !updateRemote {
		return
	}
	result.updatedInRemind++
	action := "update_reminder"
	if conflict {
		action = "conflict_keep_reflow"
	}
	result.addAction(Action{Action: action, ReflowID: localTask.ID,
		ReminderID: remoteTask.ID, Title: firstNonEmpty(localTask.Title, remoteTask.Name)})
	if localTask.DueDate == "" && remoteTask.DueDate != "" {
		result.addWarning(Action{Action: "skip_due_clear", ReflowID: localTask.ID, ReminderID: remoteTask.ID,
			Title: localTask.Title, Reason: "Apple Reminders cannot clear an existing due date; the reminder keeps it"})
	}
	result.remoteOps = append(result.remoteOps, RemoteOperation{
		Kind: "update", ReminderID: remoteTask.ID,
		Name:    firstNonEmpty(localTask.Title, "ReFlow 任务"),
		Notes:   withMarker(localTask.Notes, localTask.ID),
		DueDate: localTask.DueDate, Completed: localTask.Completed,
	})
}

func mustCleanNotes(notes string) string {
	clean, _ := splitMarker(notes)
	return clean
}

func sortRemindersForMatch(items []Reminder) {
	// Newest edits win when a damaged state file left duplicate markers.
	for i := 1; i < len(items); i++ {
		for j := i; j > 0 && items[j].ModifiedAt > items[j-1].ModifiedAt; j-- {
			items[j], items[j-1] = items[j-1], items[j]
		}
	}
}

func asPermission(err error, target **PermissionError) bool {
	value, ok := err.(*PermissionError)
	if !ok {
		return false
	}
	*target = value
	return true
}
