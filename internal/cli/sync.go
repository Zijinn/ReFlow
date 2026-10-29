package cli

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const didaSyncVersion = 1

type DidaProject struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Kind string `json:"kind"`
}

type didaTask struct {
	ID           string   `json:"id"`
	ProjectID    string   `json:"projectId"`
	Title        string   `json:"title"`
	Content      string   `json:"content"`
	Desc         string   `json:"desc"`
	StartDate    string   `json:"startDate"`
	DueDate      string   `json:"dueDate"`
	TimeZone     string   `json:"timeZone"`
	Status       int      `json:"status"`
	ModifiedTime string   `json:"modifiedTime"`
	Tags         []string `json:"tags"`
}

type DidaSyncTask struct {
	ID          string   `json:"id"`
	Title       string   `json:"title"`
	Notes       string   `json:"notes"`
	Tags        []string `json:"tags"`
	StartDate   string   `json:"start_date"`
	DueDate     string   `json:"due_date"`
	PaperID     *string  `json:"paper_id"`
	CompletedAt string   `json:"completed_at"`
	Completed   bool     `json:"completed"`
	Position    int      `json:"position"`
}

type taskSnapshot struct {
	Title     string   `json:"title"`
	Notes     string   `json:"notes"`
	Tags      []string `json:"tags"`
	StartDate string   `json:"start_date"`
	DueDate   string   `json:"due_date"`
	Completed bool     `json:"completed"`
}

type DidaSyncState struct {
	Version  int                          `json:"version"`
	Projects map[string]*DidaProjectState `json:"projects"`
}

type DidaProjectState struct {
	ProjectID   string               `json:"project_id"`
	ProjectName string               `json:"project_name"`
	Links       map[string]*didaLink `json:"links"`
	LastSync    string               `json:"last_sync,omitempty"`
}

type didaLink struct {
	DidaID     string `json:"dida_id"`
	ReflowHash string `json:"reflow_hash"`
	DidaHash   string `json:"dida_hash"`
}

type DidaSyncAction struct {
	Action   string `json:"action"`
	ReflowID string `json:"reflow_id,omitempty"`
	DidaID   string `json:"dida_id,omitempty"`
	Title    string `json:"title,omitempty"`
	Reason   string `json:"reason,omitempty"`

	local  *DidaSyncTask
	remote *didaTask
}

type DidaSyncResult struct {
	Project         DidaProject      `json:"project"`
	DryRun          bool             `json:"dry_run"`
	Seed            string           `json:"seed,omitempty"`
	StateFile       string           `json:"state_file"`
	CreatedInDida   int              `json:"created_in_dida"`
	CreatedInReflow int              `json:"created_in_reflow"`
	UpdatedInDida   int              `json:"updated_in_dida"`
	UpdatedInReflow int              `json:"updated_in_reflow"`
	Conflicts       []DidaSyncAction `json:"conflicts,omitempty"`
	Orphans         []DidaSyncAction `json:"orphans,omitempty"`
	Skipped         []DidaSyncAction `json:"skipped,omitempty"`
	Actions         []DidaSyncAction `json:"actions,omitempty"`
}

// DidaSyncStore is the ReFlow half of the bridge. The CLI and the server each
// implement it over their own transport, so both run the same mapping and
// conflict rules.
type DidaSyncStore interface {
	ListDidaSyncTasks(ctx context.Context) ([]DidaSyncTask, error)
	CreateDidaSyncTask(ctx context.Context, task DidaSyncTask) (DidaSyncTask, error)
	UpdateDidaSyncTask(ctx context.Context, id string, task DidaSyncTask) error
}

type DidaSyncOptions struct {
	Project   string
	Direction string
	Seed      string
	Command   string
	StatePath string
	DryRun    bool
}

func syncCommands() []Command {
	return []Command{
		{
			Path:     "sync dida",
			Summary:  "sync 即时任务 with a Dida365 list through the dida CLI",
			Endpoint: "GET/POST/PATCH " + quickTasksPath + " and the external dida CLI",
			Flags: []Flag{
				{Name: "project", Kind: KindString, Required: true,
					Description: "Dida list name or id; sync targets exactly this list"},
				{Name: "direction", Kind: KindChoice, Choices: []string{"merge", "push", "pull"},
					Default: "merge", Description: "merge keeps both sides, push sends ReFlow changes, pull imports Dida changes"},
				{Name: "seed", Kind: KindChoice, Choices: []string{"reflow", "dida"},
					Description: "merge first sync only: choose which side creates the initial copy when both sides already have tasks"},
				{Name: "dry-run", Kind: KindBool,
					Description: "show the plan without writing either side or updating the mapping file"},
				{Name: "dida-command", Kind: KindString, Default: "dida",
					Description: "dida CLI executable, normally found on PATH"},
			},
			Handler: runDidaSync,
		},
	}
}

func runDidaSync(ctx context.Context, app *App, v *Values, args []string) error {
	if len(args) > 0 {
		return usage("sync dida takes no positional argument, got %q", args[0])
	}
	result, err := SyncDida(ctx, cliDidaSyncStore{app: app}, DidaSyncOptions{
		Project: v.String("project"), Direction: v.String("direction"),
		Seed: v.String("seed"), Command: v.String("dida-command"), DryRun: v.Bool("dry-run"),
	})
	if err != nil {
		return err
	}
	return app.Emit(result)
}

// SyncDida performs one sync pass. Direction push/pull makes the target side
// match the source; merge does the two-way hash comparison.
func SyncDida(ctx context.Context, store DidaSyncStore, opts DidaSyncOptions) (DidaSyncResult, error) {
	direction := strings.ToLower(strings.TrimSpace(opts.Direction))
	if direction == "" {
		direction = "merge"
	}
	switch direction {
	case "merge", "push", "pull":
	default:
		return DidaSyncResult{}, usage("sync direction must be merge, push, or pull, got %q", direction)
	}
	command, err := ResolveDidaCommand(opts.Command)
	if err != nil {
		return DidaSyncResult{}, err
	}
	project, err := resolveDidaProject(ctx, command, opts.Project)
	if err != nil {
		return DidaSyncResult{}, err
	}
	statePath := strings.TrimSpace(opts.StatePath)
	if statePath == "" {
		statePath, err = didaStatePath()
		if err != nil {
			return DidaSyncResult{}, err
		}
	}
	state, err := loadDidaState(statePath)
	if err != nil {
		return DidaSyncResult{}, err
	}
	projectState := state.Projects[project.ID]
	if projectState == nil {
		projectState = &DidaProjectState{ProjectID: project.ID, ProjectName: project.Name, Links: map[string]*didaLink{}}
	}
	if projectState.Links == nil {
		projectState.Links = map[string]*didaLink{}
	}
	local, err := store.ListDidaSyncTasks(ctx)
	if err != nil {
		return DidaSyncResult{}, err
	}
	remote, err := fetchDidaTasks(ctx, command, project.ID)
	if err != nil {
		return DidaSyncResult{}, err
	}
	result := DidaSyncResult{Project: project, DryRun: opts.DryRun, StateFile: statePath}
	if direction != "merge" {
		result.Seed = "" // direction determines the first pass; seed is a merge-only choice
	}
	plan, err := planDidaSync(projectState, local, remote, direction, opts.Seed, &result)
	if err != nil {
		return DidaSyncResult{}, err
	}
	for _, action := range plan {
		if err := applyDidaAction(ctx, store, command, project, action, opts.DryRun, &result, projectState.Links); err != nil {
			return DidaSyncResult{}, err
		}
	}
	if !opts.DryRun {
		if len(plan) > 0 {
			if local, err = store.ListDidaSyncTasks(ctx); err != nil {
				return DidaSyncResult{}, err
			}
			if remote, err = fetchDidaTasks(ctx, command, project.ID); err != nil {
				return DidaSyncResult{}, err
			}
		}
		refreshDidaHashes(projectState, local, remote)
		projectState.LastSync = time.Now().UTC().Format(time.RFC3339)
		state.Projects[project.ID] = projectState
		if err := saveDidaState(statePath, state); err != nil {
			return DidaSyncResult{}, err
		}
	}
	result.Actions = plan
	result.Conflicts = actionsOf(plan, "conflict_keep_reflow")
	result.Orphans = actionsOf(plan, "orphan")
	result.Skipped = actionsOf(plan, "skip")
	return result, nil
}

// cliDidaSyncStore is the HTTP client half used by `reflow sync dida`.
type cliDidaSyncStore struct{ app *App }

func (s cliDidaSyncStore) ListDidaSyncTasks(ctx context.Context) ([]DidaSyncTask, error) {
	raw, err := s.app.client.Get(ctx, quickTasksPath, nil)
	if err != nil {
		return nil, err
	}
	var body struct {
		Items []DidaSyncTask `json:"items"`
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		return nil, &TransportError{Reason: "decode ReFlow quick tasks: " + err.Error()}
	}
	return body.Items, nil
}

func (s cliDidaSyncStore) CreateDidaSyncTask(ctx context.Context, task DidaSyncTask) (DidaSyncTask, error) {
	body := map[string]any{"title": task.Title}
	if task.Notes != "" {
		body["notes"] = task.Notes
	}
	if task.Tags != nil {
		body["tags"] = task.Tags
	}
	if task.StartDate != "" {
		body["start_date"] = task.StartDate
	}
	if task.DueDate != "" {
		body["due_date"] = task.DueDate
	}
	raw, err := s.app.client.Call(ctx, "POST", quickTasksPath, nil, body)
	if err != nil {
		return DidaSyncTask{}, err
	}
	var created DidaSyncTask
	if err := json.Unmarshal(raw, &created); err != nil {
		return DidaSyncTask{}, &TransportError{Reason: "decode ReFlow quick task create: " + err.Error()}
	}
	if created.ID == "" {
		return DidaSyncTask{}, &TransportError{Reason: "ReFlow quick task create returned no id"}
	}
	if task.Completed {
		raw, err = s.app.client.Call(ctx, "PATCH", quickTasksPath+"/"+url.PathEscape(created.ID), nil,
			map[string]any{"completed": true})
		if err != nil {
			return DidaSyncTask{}, err
		}
		if err := json.Unmarshal(raw, &created); err != nil {
			return DidaSyncTask{}, &TransportError{Reason: "decode ReFlow completed quick task: " + err.Error()}
		}
	}
	return created, nil
}

func (s cliDidaSyncStore) UpdateDidaSyncTask(ctx context.Context, id string, task DidaSyncTask) error {
	patch := map[string]any{
		"title": task.Title, "notes": task.Notes, "tags": task.Tags,
		"start_date": task.StartDate, "due_date": task.DueDate, "completed": task.Completed,
	}
	_, err := s.app.client.Call(ctx, "PATCH", quickTasksPath+"/"+url.PathEscape(id), nil, patch)
	return err
}

func actionsOf(actions []DidaSyncAction, kind string) []DidaSyncAction {
	var out []DidaSyncAction
	for _, action := range actions {
		if action.Action == kind {
			out = append(out, action)
		}
	}
	return out
}

// ResolveDidaCommand finds the dida executable. GUI-launched processes often get
// a short PATH, so the npm/Homebrew locations are checked as well.
func ResolveDidaCommand(command string) (string, error) {
	command = strings.TrimSpace(command)
	if command == "" {
		command = "dida"
	}
	if resolved, err := exec.LookPath(command); err == nil {
		return resolved, nil
	}
	if strings.ContainsRune(command, os.PathSeparator) {
		return "", usage("dida CLI not found (%s)", command)
	}
	candidates := []string{"/opt/homebrew/bin/" + command, "/usr/local/bin/" + command}
	if home, err := os.UserHomeDir(); err == nil {
		candidates = append(candidates, filepath.Join(home, ".local", "bin", command))
	}
	for _, candidate := range candidates {
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			return candidate, nil
		}
	}
	return "", usage("dida CLI not found (%s): install it with `npm install -g @suibiji/dida-cli` "+
		"or pass --dida-command", command)
}

// --- dida CLI transport ---

func runDidaJSON(ctx context.Context, command string, args ...string) (json.RawMessage, error) {
	full := append(append([]string{}, args...), "--json")
	cmd := exec.CommandContext(ctx, command, full...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		reason := strings.TrimSpace(stderr.String())
		if reason == "" {
			reason = err.Error()
		}
		return nil, &TransportError{Reason: "dida " + strings.Join(args, " ") + ": " + reason}
	}
	raw := bytes.TrimSpace(stdout.Bytes())
	if len(raw) == 0 || !json.Valid(raw) {
		return nil, &TransportError{Reason: "dida " + strings.Join(args, " ") + " did not return JSON"}
	}
	return json.RawMessage(raw), nil
}

func resolveDidaProject(ctx context.Context, command, target string) (DidaProject, error) {
	target = strings.TrimSpace(target)
	if target == "" {
		return DidaProject{}, usage("--project needs a Dida list name or id")
	}
	raw, err := runDidaJSON(ctx, command, "project", "list")
	if err != nil {
		return DidaProject{}, err
	}
	var projects []DidaProject
	if err := json.Unmarshal(raw, &projects); err != nil {
		return DidaProject{}, &TransportError{Reason: "decode dida project list: " + err.Error()}
	}
	var matches []DidaProject
	for _, project := range projects {
		if project.ID == target || project.Name == target {
			matches = append(matches, project)
		}
	}
	if len(matches) == 0 {
		for _, project := range projects {
			if strings.EqualFold(project.Name, target) {
				matches = append(matches, project)
			}
		}
	}
	if len(matches) == 0 {
		names := make([]string, 0, len(projects))
		for _, project := range projects {
			names = append(names, project.Name)
		}
		return DidaProject{}, usage("Dida list %q not found; available: %s",
			target, strings.Join(names, ", "))
	}
	if len(matches) > 1 {
		return DidaProject{}, usage("Dida list name %q is ambiguous; use its id", target)
	}
	return matches[0], nil
}

func fetchDidaTasks(ctx context.Context, command, projectID string) ([]didaTask, error) {
	raw, err := runDidaJSON(ctx, command, "task", "filter",
		"--projects", projectID, "--status=-1,0,2")
	if err != nil {
		return nil, err
	}
	var tasks []didaTask
	if err := json.Unmarshal(raw, &tasks); err == nil {
		return tasks, nil
	}
	var wrapper struct {
		Tasks []didaTask `json:"tasks"`
	}
	if err := json.Unmarshal(raw, &wrapper); err != nil {
		return nil, &TransportError{Reason: "decode dida task list: " + err.Error()}
	}
	return wrapper.Tasks, nil
}

func fetchReflowQuickTasks(ctx context.Context, app *App) ([]DidaSyncTask, error) {
	raw, err := app.client.Get(ctx, quickTasksPath, nil)
	if err != nil {
		return nil, err
	}
	var body struct {
		Items []DidaSyncTask `json:"items"`
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		return nil, &TransportError{Reason: "decode ReFlow quick tasks: " + err.Error()}
	}
	return body.Items, nil
}

// --- snapshots ---

func localSnapshot(task DidaSyncTask) taskSnapshot {
	tags := append([]string(nil), task.Tags...)
	sort.Strings(tags)
	return taskSnapshot{
		Title: strings.TrimSpace(task.Title), Notes: strings.TrimSpace(task.Notes), Tags: tags,
		StartDate: task.StartDate, DueDate: task.DueDate, Completed: task.Completed,
	}
}

func remoteSnapshot(task didaTask, fallbackTags []string) taskSnapshot {
	tags := fallbackTags
	if task.Tags != nil {
		tags = append([]string(nil), task.Tags...)
	}
	tags = append([]string(nil), tags...)
	sort.Strings(tags)
	notes := strings.TrimSpace(task.Content)
	if notes == "" {
		notes = strings.TrimSpace(task.Desc)
	}
	return taskSnapshot{
		Title: strings.TrimSpace(task.Title), Notes: notes, Tags: tags,
		StartDate: didaDateToReflow(task.StartDate, task.TimeZone), DueDate: didaDateToReflow(task.DueDate, task.TimeZone),
		Completed: task.Status == 2,
	}
}

func snapshotHash(snapshot taskSnapshot) string {
	body, _ := json.Marshal(snapshot)
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:])
}

func sameSnapshot(left, right taskSnapshot) bool {
	return snapshotHash(left) == snapshotHash(right)
}

// --- state file ---

func didaStatePath() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "reflow", "dida-sync.json"), nil
}

func loadDidaState(path string) (DidaSyncState, error) {
	state := DidaSyncState{Version: didaSyncVersion, Projects: map[string]*DidaProjectState{}}
	body, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return state, nil
	}
	if err != nil {
		return state, err
	}
	if err := json.Unmarshal(body, &state); err != nil {
		return state, fmt.Errorf("decode %s: %w", path, err)
	}
	state.Version = didaSyncVersion
	if state.Projects == nil {
		state.Projects = map[string]*DidaProjectState{}
	}
	return state, nil
}

func saveDidaState(path string, state DidaSyncState) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	body, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	temporary, err := os.CreateTemp(dir, ".dida-sync-*.tmp")
	if err != nil {
		return err
	}
	name := temporary.Name()
	defer os.Remove(name)
	if err := temporary.Chmod(0o600); err != nil {
		temporary.Close()
		return err
	}
	if _, err := temporary.Write(append(body, '\n')); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	return os.Rename(name, path)
}

// --- planning ---

func planDidaSync(project *DidaProjectState, local []DidaSyncTask, remote []didaTask, direction, seed string, result *DidaSyncResult) ([]DidaSyncAction, error) {
	localByID := make(map[string]DidaSyncTask, len(local))
	for _, task := range local {
		localByID[task.ID] = task
	}
	remoteByID := make(map[string]didaTask, len(remote))
	for _, task := range remote {
		remoteByID[task.ID] = task
	}

	if len(project.Links) == 0 {
		if len(local) == 0 && len(remote) == 0 {
			return nil, nil
		}
		switch direction {
		case "push":
			seed = "reflow"
		case "pull":
			seed = "dida"
		default:
			if seed == "" {
				switch {
				case len(local) == 0:
					seed = "dida"
				case len(remote) == 0:
					seed = "reflow"
				default:
					return nil, usage("both ReFlow and the Dida list already contain tasks; " +
						"choose --seed reflow or --seed dida for the first merge")
				}
			}
		}
		result.Seed = seed
		actions := make([]DidaSyncAction, 0, len(local)+len(remote))
		switch seed {
		case "reflow":
			for _, task := range sortedQuickTasks(local) {
				copied := task
				actions = append(actions, DidaSyncAction{Action: "create_dida", ReflowID: task.ID, Title: task.Title, local: &copied})
			}
		case "dida":
			for _, task := range sortedDidaTasks(remote) {
				if task.Status == -1 {
					actions = append(actions, DidaSyncAction{Action: "skip", DidaID: task.ID, Title: task.Title, Reason: "Dida task is abandoned (status -1)"})
					continue
				}
				copied := task
				actions = append(actions, DidaSyncAction{Action: "create_reflow", DidaID: task.ID, Title: task.Title, remote: &copied})
			}
		default:
			return nil, usage("--seed must be reflow or dida, got %q", seed)
		}
		return actions, nil
	}

	actions := make([]DidaSyncAction, 0)
	linkedReflow := make(map[string]struct{}, len(project.Links))
	for _, reflowID := range sortedLinkIDs(project.Links) {
		link := project.Links[reflowID]
		if link == nil {
			delete(project.Links, reflowID)
			continue
		}
		linkedReflow[reflowID] = struct{}{}
		localTask, hasLocal := localByID[reflowID]
		remoteTask, hasRemote := remoteByID[link.DidaID]
		if !hasLocal && !hasRemote {
			delete(project.Links, reflowID)
			continue
		}
		if !hasLocal || !hasRemote {
			action := DidaSyncAction{Action: "orphan", ReflowID: reflowID, DidaID: link.DidaID,
				Title: firstNonEmpty(localTask.Title, remoteTask.Title)}
			if !hasLocal {
				action.Reason = "ReFlow task is missing; Dida copy left untouched"
			} else {
				action.Reason = "Dida task is missing; ReFlow copy left untouched"
			}
			actions = append(actions, action)
			continue
		}
		if remoteTask.Status == -1 {
			actions = append(actions, DidaSyncAction{Action: "skip", ReflowID: reflowID, DidaID: link.DidaID,
				Title: localTask.Title, Reason: "Dida task is abandoned (status -1)"})
			continue
		}
		localSnap := localSnapshot(localTask)
		remoteSnap := remoteSnapshot(remoteTask, localTask.Tags)
		localChanged := snapshotHash(localSnap) != link.ReflowHash
		remoteChanged := snapshotHash(remoteSnap) != link.DidaHash
		if !localChanged && !remoteChanged {
			continue
		}
		localCopy, remoteCopy := localTask, remoteTask
		if direction == "push" {
			if !sameSnapshot(localSnap, remoteSnap) {
				actions = append(actions, DidaSyncAction{Action: "update_dida", ReflowID: reflowID, DidaID: link.DidaID,
					Title: localTask.Title, local: &localCopy, remote: &remoteCopy})
			}
			continue
		}
		if direction == "pull" {
			if !sameSnapshot(localSnap, remoteSnap) {
				actions = append(actions, DidaSyncAction{Action: "update_reflow", ReflowID: reflowID, DidaID: link.DidaID,
					Title: remoteTask.Title, local: &localCopy, remote: &remoteCopy})
			}
			continue
		}
		switch {
		case localChanged && !remoteChanged:
			actions = append(actions, DidaSyncAction{Action: "update_dida", ReflowID: reflowID, DidaID: link.DidaID,
				Title: localTask.Title, local: &localCopy, remote: &remoteCopy})
		case !localChanged && remoteChanged:
			actions = append(actions, DidaSyncAction{Action: "update_reflow", ReflowID: reflowID, DidaID: link.DidaID,
				Title: remoteTask.Title, local: &localCopy, remote: &remoteCopy})
		case sameSnapshot(localSnap, remoteSnap):
			// Both changed but converged; refresh the stored hashes without writing.
		default:
			actions = append(actions, DidaSyncAction{Action: "conflict_keep_reflow", ReflowID: reflowID, DidaID: link.DidaID,
				Title: localTask.Title, local: &localCopy, remote: &remoteCopy})
		}
	}

	if direction != "pull" {
		for _, task := range sortedQuickTasks(local) {
			if _, linked := linkedReflow[task.ID]; linked {
				continue
			}
			copied := task
			actions = append(actions, DidaSyncAction{Action: "create_dida", ReflowID: task.ID, Title: task.Title, local: &copied})
		}
	}
	linkedDida := make(map[string]struct{}, len(project.Links))
	for _, link := range project.Links {
		if link != nil {
			linkedDida[link.DidaID] = struct{}{}
		}
	}
	if direction != "push" {
		for _, task := range sortedDidaTasks(remote) {
			if _, linked := linkedDida[task.ID]; linked {
				continue
			}
			if task.Status == -1 {
				actions = append(actions, DidaSyncAction{Action: "skip", DidaID: task.ID, Title: task.Title, Reason: "Dida task is abandoned (status -1)"})
				continue
			}
			copied := task
			actions = append(actions, DidaSyncAction{Action: "create_reflow", DidaID: task.ID, Title: task.Title, remote: &copied})
		}
	}
	return actions, nil
}

func sortedQuickTasks(tasks []DidaSyncTask) []DidaSyncTask {
	out := append([]DidaSyncTask(nil), tasks...)
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Position != out[j].Position {
			return out[i].Position < out[j].Position
		}
		return out[i].ID < out[j].ID
	})
	return out
}

func sortedDidaTasks(tasks []didaTask) []didaTask {
	out := append([]didaTask(nil), tasks...)
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].ModifiedTime != out[j].ModifiedTime {
			return out[i].ModifiedTime < out[j].ModifiedTime
		}
		return out[i].ID < out[j].ID
	})
	return out
}

func sortedLinkIDs(links map[string]*didaLink) []string {
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

// --- applying ---

func applyDidaAction(ctx context.Context, store DidaSyncStore, command string, project DidaProject, action DidaSyncAction, dryRun bool, result *DidaSyncResult, links map[string]*didaLink) error {
	if action.Action == "orphan" || action.Action == "skip" {
		return nil
	}
	if dryRun {
		return nil
	}
	switch action.Action {
	case "create_dida":
		if action.local == nil {
			return errors.New("sync plan lost the ReFlow task for a Dida create")
		}
		created, err := createDidaTask(ctx, command, project.ID, *action.local)
		if err != nil {
			return err
		}
		result.CreatedInDida++
		links[action.ReflowID] = &didaLink{DidaID: created.ID}
	case "create_reflow":
		if action.remote == nil {
			return errors.New("sync plan lost the Dida task for a ReFlow create")
		}
		created, err := createReflowTask(ctx, store, *action.remote)
		if err != nil {
			return err
		}
		result.CreatedInReflow++
		links[created.ID] = &didaLink{DidaID: action.remote.ID}
	case "update_dida", "conflict_keep_reflow":
		if action.local == nil || action.remote == nil {
			return errors.New("sync plan lost a task for a Dida update")
		}
		if err := updateDidaTask(ctx, command, project.ID, *action.local, *action.remote); err != nil {
			return err
		}
		result.UpdatedInDida++
	case "update_reflow":
		if action.local == nil || action.remote == nil {
			return errors.New("sync plan lost a task for a ReFlow update")
		}
		if err := updateReflowTask(ctx, store, *action.local, *action.remote); err != nil {
			return err
		}
		result.UpdatedInReflow++
	}
	return nil
}

func createDidaTask(ctx context.Context, command, projectID string, local DidaSyncTask) (didaTask, error) {
	title := strings.TrimSpace(local.Title)
	if title == "" {
		title = "ReFlow 任务"
	}
	args := []string{"task", "create", "--title", title, "--project", projectID}
	if notes := strings.TrimSpace(local.Notes); notes != "" {
		args = append(args, "--content", notes)
	}
	if len(local.Tags) > 0 {
		args = append(args, "--tags", strings.Join(local.Tags, ","))
	}
	args = appendDidaCreateDates(args, local.StartDate, local.DueDate)
	raw, err := runDidaJSON(ctx, command, args...)
	if err != nil {
		return didaTask{}, err
	}
	var created didaTask
	if err := json.Unmarshal(raw, &created); err != nil {
		return didaTask{}, &TransportError{Reason: "decode dida task create: " + err.Error()}
	}
	if created.ID == "" {
		return didaTask{}, &TransportError{Reason: "dida task create returned no id"}
	}
	if local.Completed {
		if err := setDidaStatus(ctx, command, projectID, created.ID, 2); err != nil {
			return didaTask{}, err
		}
		created.Status = 2
	}
	return created, nil
}

func updateDidaTask(ctx context.Context, command, projectID string, local DidaSyncTask, remote didaTask) error {
	title := strings.TrimSpace(local.Title)
	if title == "" {
		title = "ReFlow 任务"
	}
	args := []string{"task", "update", remote.ID, "--id", remote.ID, "--project", projectID,
		"--title", title, "--content", strings.TrimSpace(local.Notes)}
	if len(local.Tags) > 0 {
		args = append(args, "--tags", strings.Join(local.Tags, ","))
	}
	args = appendDidaUpdateDates(args, local.StartDate, local.DueDate)
	if local.Completed {
		args = append(args, "--status", "2")
	} else {
		args = append(args, "--status", "0")
	}
	_, err := runDidaJSON(ctx, command, args...)
	return err
}

func setDidaStatus(ctx context.Context, command, projectID, taskID string, status int) error {
	_, err := runDidaJSON(ctx, command, "task", "update", taskID, "--id", taskID,
		"--project", projectID, "--status", fmt.Sprint(status))
	return err
}

func appendDidaCreateDates(args []string, start, due string) []string {
	if start != "" {
		args = append(args, "--start-date", reflowDateToDida(start))
	}
	if due != "" {
		args = append(args, "--due-date", reflowDateToDida(due))
	}
	if start != "" || due != "" {
		args = append(args, "--all-day")
	}
	return args
}

func appendDidaUpdateDates(args []string, start, due string) []string {
	if start != "" {
		args = append(args, "--start-date", reflowDateToDida(start))
	} else {
		args = append(args, "--start-date", "none")
	}
	if due != "" {
		args = append(args, "--due-date", reflowDateToDida(due))
	} else {
		args = append(args, "--due-date", "null")
	}
	if start != "" || due != "" {
		args = append(args, "--all-day")
	}
	return args
}

func createReflowTask(ctx context.Context, store DidaSyncStore, remote didaTask) (DidaSyncTask, error) {
	title := strings.TrimSpace(remote.Title)
	if title == "" {
		title = "Dida 任务"
	}
	task := DidaSyncTask{
		Title: title, Notes: firstNonEmpty(strings.TrimSpace(remote.Content), strings.TrimSpace(remote.Desc)),
		Tags: remote.Tags, StartDate: didaDateToReflow(remote.StartDate, remote.TimeZone),
		DueDate: didaDateToReflow(remote.DueDate, remote.TimeZone), Completed: remote.Status == 2,
	}
	return store.CreateDidaSyncTask(ctx, task)
}

func updateReflowTask(ctx context.Context, store DidaSyncStore, local DidaSyncTask, remote didaTask) error {
	title := strings.TrimSpace(remote.Title)
	if title == "" {
		title = local.Title
	}
	tags := remote.Tags
	if tags == nil {
		tags = local.Tags
	}
	task := DidaSyncTask{
		Title: title, Notes: firstNonEmpty(strings.TrimSpace(remote.Content), strings.TrimSpace(remote.Desc)),
		Tags: tags, StartDate: didaDateToReflow(remote.StartDate, remote.TimeZone),
		DueDate: didaDateToReflow(remote.DueDate, remote.TimeZone), Completed: remote.Status == 2,
	}
	return store.UpdateDidaSyncTask(ctx, local.ID, task)
}

func refreshDidaHashes(project *DidaProjectState, local []DidaSyncTask, remote []didaTask) {
	localByID := make(map[string]DidaSyncTask, len(local))
	for _, task := range local {
		localByID[task.ID] = task
	}
	remoteByID := make(map[string]didaTask, len(remote))
	for _, task := range remote {
		remoteByID[task.ID] = task
	}
	for reflowID, link := range project.Links {
		if link == nil {
			continue
		}
		localTask, hasLocal := localByID[reflowID]
		remoteTask, hasRemote := remoteByID[link.DidaID]
		if !hasLocal || !hasRemote {
			continue
		}
		link.ReflowHash = snapshotHash(localSnapshot(localTask))
		link.DidaHash = snapshotHash(remoteSnapshot(remoteTask, localTask.Tags))
	}
}

func didaDateToReflow(value, zone string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	layouts := []string{
		time.RFC3339Nano,
		"2006-01-02T15:04:05.000-0700",
		"2006-01-02T15:04:05-0700",
		"2006-01-02 15:04:05",
	}
	for _, layout := range layouts {
		parsed, err := time.Parse(layout, value)
		if err != nil {
			continue
		}
		location := time.Local
		if zone != "" {
			if loaded, loadErr := time.LoadLocation(zone); loadErr == nil {
				location = loaded
			}
		}
		return parsed.In(location).Format("2006-01-02")
	}
	if len(value) >= 10 {
		candidate := value[:10]
		if _, err := time.Parse("2006-01-02", candidate); err == nil {
			return candidate
		}
	}
	return ""
}

func reflowDateToDida(value string) string {
	parsed, err := time.Parse("2006-01-02", value)
	if err != nil {
		return value
	}
	local := time.Date(parsed.Year(), parsed.Month(), parsed.Day(), 0, 0, 0, 0, time.Local)
	return local.Format("2006-01-02T15:04:05-0700")
}
