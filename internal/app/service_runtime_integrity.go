package app

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/doeshing/nekoclaw/internal/core"
	"github.com/doeshing/nekoclaw/internal/mcp"
	"github.com/doeshing/nekoclaw/internal/provider"
	"github.com/doeshing/nekoclaw/internal/tooling"
)

const workspaceGrantTTL = 7 * 24 * time.Hour

type snapshotMode string

const (
	snapshotModeGit   snapshotMode = "git"
	snapshotModeFiles snapshotMode = "files"
)

type snapshotFileBackup struct {
	RelPath    string `json:"rel_path"`
	BackupPath string `json:"backup_path,omitempty"`
	Existed    bool   `json:"existed"`
}

type snapshotManifest struct {
	SnapshotID    string               `json:"snapshot_id"`
	SessionID     string               `json:"session_id"`
	WorkspaceRoot string               `json:"workspace_root"`
	Mode          snapshotMode         `json:"mode"`
	RepoRoot      string               `json:"repo_root,omitempty"`
	CreatedAt     time.Time            `json:"created_at"`
	IndexPatch    string               `json:"index_patch,omitempty"`
	WorktreePatch string               `json:"worktree_patch,omitempty"`
	Untracked     []snapshotFileBackup `json:"untracked,omitempty"`
	Files         []snapshotFileBackup `json:"files,omitempty"`
	FullWorkspace bool                 `json:"full_workspace,omitempty"`
}

func (s *Service) GetSnapshot(snapshotID string) (core.SnapshotRecord, error) {
	if s.snapshots == nil {
		return core.SnapshotRecord{}, fmt.Errorf("snapshot store unavailable")
	}
	record, ok := s.snapshots.Get(snapshotID)
	if !ok {
		return core.SnapshotRecord{}, fmt.Errorf("snapshot not found")
	}
	return record, nil
}

func (s *Service) toolCallSelector(call provider.ToolCall) string {
	name := strings.TrimSpace(call.Name)
	if name == "" {
		return ""
	}
	if mcp.IsMCPTool(name) {
		return name
	}
	resolvePath := func(raw string) string {
		policy := tooling.DefaultPolicy(s.workspaceRoot)
		full, err := policy.ResolvePath(strings.TrimSpace(raw))
		if err != nil {
			return ""
		}
		return full
	}

	switch name {
	case "file_read", "file_write", "file_replace":
		var args struct {
			Path string `json:"path"`
		}
		if err := json.Unmarshal(call.Arguments, &args); err != nil {
			return ""
		}
		return resolvePath(args.Path)
	case "git_add":
		return "add"
	case "git_restore":
		return "restore"
	case "git_commit":
		return "commit"
	case "exec_command":
		var args struct {
			Argv []string `json:"argv"`
		}
		if err := json.Unmarshal(call.Arguments, &args); err != nil {
			return ""
		}
		if len(args.Argv) == 0 {
			return ""
		}
		parts := make([]string, 0, 2)
		for _, item := range args.Argv {
			item = strings.TrimSpace(item)
			if item == "" {
				continue
			}
			parts = append(parts, item)
			if len(parts) >= 2 {
				break
			}
		}
		return strings.Join(parts, " ")
	default:
		return ""
	}
}

func (s *Service) lookupToolGrant(sessionID, toolName, selector string) (string, bool) {
	key := toolGrantKey(toolName, selector)
	s.sessionGrantsMu.Lock()
	if grants := s.sessionGrants[strings.TrimSpace(sessionID)]; grants != nil {
		if decision, ok := grants[key]; ok {
			s.sessionGrantsMu.Unlock()
			return decision, true
		}
	}
	s.sessionGrantsMu.Unlock()

	if s.permissions == nil {
		return "", false
	}
	matches := s.permissions.Match(workspaceScopeValue(s.workspaceRoot), strings.TrimSpace(toolName), strings.TrimSpace(selector))
	if len(matches) == 0 {
		return "", false
	}
	sort.Slice(matches, func(i, j int) bool {
		return matches[i].CreatedAt.After(matches[j].CreatedAt)
	})
	return normalizeGrantDecision(matches[0].Decision), true
}

func (s *Service) persistToolGrant(sessionID, toolName, selector, decision string) error {
	decision = normalizeGrantDecision(decision)
	switch decision {
	case "allow_for_session":
		s.sessionGrantsMu.Lock()
		defer s.sessionGrantsMu.Unlock()
		sessionID = strings.TrimSpace(sessionID)
		if sessionID == "" {
			sessionID = "main"
		}
		if s.sessionGrants[sessionID] == nil {
			s.sessionGrants[sessionID] = map[string]string{}
		}
		s.sessionGrants[sessionID][toolGrantKey(toolName, selector)] = decision
		return nil
	case "allow_for_workspace":
		if s.permissions == nil {
			return fmt.Errorf("permission store unavailable")
		}
		expiresAt := time.Now().UTC().Add(workspaceGrantTTL)
		_, err := s.permissions.Grant(core.PermissionGrant{
			WorkspaceRoot: workspaceScopeValue(s.workspaceRoot),
			ToolName:      strings.TrimSpace(toolName),
			Selector:      strings.TrimSpace(selector),
			Decision:      decision,
			ExpiresAt:     &expiresAt,
		})
		return err
	default:
		return nil
	}
}

func normalizeGrantDecision(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "", "allow", "allow_once":
		return "allow_once"
	case "allow_for_session":
		return "allow_for_session"
	case "allow_for_workspace":
		return "allow_for_workspace"
	case "deny":
		return "deny"
	default:
		return strings.ToLower(strings.TrimSpace(raw))
	}
}

func toolGrantKey(toolName, selector string) string {
	return strings.TrimSpace(toolName) + "::" + strings.TrimSpace(selector)
}

func (s *Service) prepareToolMutation(sessionID string, call provider.ToolCall, snapshotID string) (*core.SnapshotRecord, error) {
	if s.snapshots == nil || strings.TrimSpace(s.workspaceRoot) == "" {
		return nil, nil
	}

	var (
		record  core.SnapshotRecord
		created *core.SnapshotRecord
		err     error
	)
	if strings.TrimSpace(snapshotID) == "" {
		record, err = s.createWorkspaceSnapshot(sessionID, call.Name)
		if err != nil {
			return nil, err
		}
		created = &record
	} else {
		record, err = s.GetSnapshot(snapshotID)
		if err != nil {
			return nil, err
		}
	}

	manifest, err := s.loadSnapshotManifest(record)
	if err != nil {
		return nil, err
	}
	if manifest.Mode == snapshotModeFiles {
		if err := s.captureFileModeSnapshotTargets(record, &manifest, call); err != nil {
			return nil, err
		}
		if err := s.saveSnapshotManifest(record, manifest); err != nil {
			return nil, err
		}
	}
	return created, nil
}

func (s *Service) UndoSnapshot(snapshotID string) (core.SnapshotRecord, bool, string, error) {
	record, err := s.GetSnapshot(snapshotID)
	if err != nil {
		return core.SnapshotRecord{}, false, "", err
	}
	if record.RestoredAt != nil {
		return record, false, "snapshot already restored", nil
	}
	if !s.isLatestRestorableSnapshot(record.SessionID, record.ID) {
		return core.SnapshotRecord{}, false, "", fmt.Errorf("snapshot is not the latest restorable snapshot")
	}

	manifest, err := s.loadSnapshotManifest(record)
	if err != nil {
		return core.SnapshotRecord{}, false, "", err
	}
	switch manifest.Mode {
	case snapshotModeGit:
		err = s.restoreGitSnapshot(record, manifest)
	case snapshotModeFiles:
		err = s.restoreFileSnapshot(record, manifest)
	default:
		err = fmt.Errorf("unknown snapshot mode: %s", manifest.Mode)
	}
	if err != nil {
		return core.SnapshotRecord{}, false, "", err
	}

	now := time.Now().UTC()
	record.RestoredAt = &now
	if err := s.snapshots.Update(record); err != nil {
		return core.SnapshotRecord{}, false, "", err
	}

	message := fmt.Sprintf("已還原 snapshot `%s`（%s）。", record.ID, strings.TrimSpace(record.ToolName))
	if s.sessions != nil && strings.TrimSpace(record.SessionID) != "" {
		s.sessions.AppendMessage(record.SessionID, core.Message{
			Role:      core.RoleAssistant,
			Content:   message,
			CreatedAt: now,
		})
	}
	return record, true, message, nil
}

func (s *Service) isLatestRestorableSnapshot(sessionID, snapshotID string) bool {
	for _, record := range s.ListSnapshots(sessionID) {
		if record.RestoredAt != nil {
			continue
		}
		return record.ID == strings.TrimSpace(snapshotID)
	}
	return false
}

func (s *Service) createWorkspaceSnapshot(sessionID, toolName string) (core.SnapshotRecord, error) {
	record, err := s.snapshots.Put(core.SnapshotRecord{
		SessionID:     strings.TrimSpace(sessionID),
		WorkspaceRoot: workspaceScopeValue(s.workspaceRoot),
		ToolName:      strings.TrimSpace(toolName),
	})
	if err != nil {
		return core.SnapshotRecord{}, err
	}
	if artifactDir := s.snapshots.ArtifactDir(record.ID); artifactDir != "" {
		record.Artifact = filepath.ToSlash(filepath.Join("artifacts", record.ID, "manifest.json"))
	} else {
		tempDir, err := os.MkdirTemp("", "nekoclaw-snapshot-"+record.ID+"-")
		if err != nil {
			return core.SnapshotRecord{}, err
		}
		record.Artifact = filepath.Join(tempDir, "manifest.json")
	}
	if err := s.snapshots.Update(record); err != nil {
		return core.SnapshotRecord{}, err
	}

	manifest := snapshotManifest{
		SnapshotID:    record.ID,
		SessionID:     record.SessionID,
		WorkspaceRoot: s.workspaceRoot,
		CreatedAt:     time.Now().UTC(),
	}
	if repoRoot, ok := detectGitRepoRoot(s.workspaceRoot); ok {
		manifest.Mode = snapshotModeGit
		manifest.RepoRoot = repoRoot
		if err := s.captureGitSnapshot(record, &manifest); err != nil {
			return core.SnapshotRecord{}, err
		}
	} else {
		manifest.Mode = snapshotModeFiles
	}
	if err := s.saveSnapshotManifest(record, manifest); err != nil {
		return core.SnapshotRecord{}, err
	}
	return record, nil
}

func (s *Service) captureGitSnapshot(record core.SnapshotRecord, manifest *snapshotManifest) error {
	artifactDir, err := s.snapshotArtifactDir(record)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(artifactDir, 0o755); err != nil {
		return err
	}

	indexPatch, err := runCommandOutput(manifest.RepoRoot, "git", "diff", "--binary", "--cached")
	if err != nil {
		return err
	}
	worktreePatch, err := runCommandOutput(manifest.RepoRoot, "git", "diff", "--binary")
	if err != nil {
		return err
	}
	manifest.IndexPatch = "index.patch"
	manifest.WorktreePatch = "worktree.patch"
	if err := os.WriteFile(filepath.Join(artifactDir, manifest.IndexPatch), indexPatch, 0o644); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(artifactDir, manifest.WorktreePatch), worktreePatch, 0o644); err != nil {
		return err
	}

	out, err := runCommandOutput(manifest.RepoRoot, "git", "ls-files", "--others", "--exclude-standard", "-z")
	if err != nil {
		return err
	}
	manifest.Untracked = nil
	for _, item := range bytes.Split(out, []byte{0}) {
		rel := strings.TrimSpace(string(item))
		if rel == "" {
			continue
		}
		entry, err := captureBackupFile(filepath.Join(manifest.RepoRoot, rel), filepath.Join(artifactDir, "untracked"), manifest.RepoRoot)
		if err != nil {
			return err
		}
		manifest.Untracked = append(manifest.Untracked, entry)
	}
	return nil
}

func (s *Service) captureFileModeSnapshotTargets(record core.SnapshotRecord, manifest *snapshotManifest, call provider.ToolCall) error {
	artifactDir, err := s.snapshotArtifactDir(record)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(artifactDir, 0o755); err != nil {
		return err
	}

	if path := s.toolCallSelector(call); path != "" && isFileMutationCall(call.Name) {
		entry, err := captureBackupFile(path, filepath.Join(artifactDir, "files"), s.workspaceRoot)
		if err != nil {
			return err
		}
		if appendBackupEntry(&manifest.Files, entry) {
			return nil
		}
		return nil
	}
	if manifest.FullWorkspace {
		return nil
	}
	filesRoot := filepath.Join(artifactDir, "files")
	if err := filepath.WalkDir(s.workspaceRoot, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			return nil
		}
		entry, err := captureBackupFile(path, filesRoot, s.workspaceRoot)
		if err != nil {
			return err
		}
		appendBackupEntry(&manifest.Files, entry)
		return nil
	}); err != nil {
		return err
	}
	manifest.FullWorkspace = true
	return nil
}

func appendBackupEntry(entries *[]snapshotFileBackup, entry snapshotFileBackup) bool {
	for _, existing := range *entries {
		if existing.RelPath == entry.RelPath {
			return true
		}
	}
	*entries = append(*entries, entry)
	return false
}

func captureBackupFile(fullPath, backupRoot, relBase string) (snapshotFileBackup, error) {
	rel, err := filepath.Rel(relBase, fullPath)
	if err != nil {
		return snapshotFileBackup{}, err
	}
	rel = filepath.Clean(rel)
	info, statErr := os.Stat(fullPath)
	if statErr != nil && !os.IsNotExist(statErr) {
		return snapshotFileBackup{}, statErr
	}
	entry := snapshotFileBackup{
		RelPath: filepath.ToSlash(rel),
		Existed: statErr == nil,
	}
	if statErr != nil && os.IsNotExist(statErr) {
		return entry, nil
	}
	if info.IsDir() {
		return entry, nil
	}
	backupPath := filepath.Join(backupRoot, rel)
	if err := os.MkdirAll(filepath.Dir(backupPath), 0o755); err != nil {
		return snapshotFileBackup{}, err
	}
	if err := copyFile(fullPath, backupPath); err != nil {
		return snapshotFileBackup{}, err
	}
	entry.BackupPath = filepath.ToSlash(strings.TrimPrefix(strings.TrimPrefix(backupPath, backupRoot), string(filepath.Separator)))
	return entry, nil
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()
	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return out.Close()
}

func (s *Service) loadSnapshotManifest(record core.SnapshotRecord) (snapshotManifest, error) {
	artifactDir, err := s.snapshotArtifactDir(record)
	if err != nil {
		return snapshotManifest{}, err
	}
	content, err := os.ReadFile(filepath.Join(artifactDir, "manifest.json"))
	if err != nil {
		return snapshotManifest{}, err
	}
	var manifest snapshotManifest
	if err := json.Unmarshal(content, &manifest); err != nil {
		return snapshotManifest{}, err
	}
	return manifest, nil
}

func (s *Service) saveSnapshotManifest(record core.SnapshotRecord, manifest snapshotManifest) error {
	artifactDir, err := s.snapshotArtifactDir(record)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(artifactDir, 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(artifactDir, "manifest.json"), append(data, '\n'), 0o644)
}

func (s *Service) restoreGitSnapshot(record core.SnapshotRecord, manifest snapshotManifest) error {
	if manifest.RepoRoot == "" {
		return fmt.Errorf("snapshot missing repo root")
	}
	artifactDir, err := s.snapshotArtifactDir(record)
	if err != nil {
		return err
	}
	if _, err := runCommandOutput(manifest.RepoRoot, "git", "reset", "--hard", "HEAD"); err != nil {
		return err
	}
	if _, err := runCommandOutput(manifest.RepoRoot, "git", "clean", "-fd"); err != nil {
		return err
	}
	if err := applyPatchIfPresent(manifest.RepoRoot, filepath.Join(artifactDir, manifest.IndexPatch), true); err != nil {
		return err
	}
	if err := applyPatchIfPresent(manifest.RepoRoot, filepath.Join(artifactDir, manifest.WorktreePatch), false); err != nil {
		return err
	}
	for _, entry := range manifest.Untracked {
		if !entry.Existed {
			continue
		}
		src := filepath.Join(artifactDir, "untracked", filepath.FromSlash(entry.RelPath))
		dst := filepath.Join(manifest.RepoRoot, filepath.FromSlash(entry.RelPath))
		if err := copyFile(src, dst); err != nil {
			return err
		}
	}
	return nil
}

func applyPatchIfPresent(repoRoot, patchPath string, index bool) error {
	content, err := os.ReadFile(patchPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if len(bytes.TrimSpace(content)) == 0 {
		return nil
	}
	argv := []string{"git", "apply", "--whitespace=nowarn"}
	if index {
		argv = append(argv, "--index")
	}
	argv = append(argv, patchPath)
	_, err = runCommandOutput(repoRoot, argv...)
	return err
}

func (s *Service) restoreFileSnapshot(record core.SnapshotRecord, manifest snapshotManifest) error {
	artifactDir, err := s.snapshotArtifactDir(record)
	if err != nil {
		return err
	}
	expected := map[string]snapshotFileBackup{}
	for _, entry := range manifest.Files {
		expected[entry.RelPath] = entry
		if !entry.Existed {
			_ = os.Remove(filepath.Join(manifest.WorkspaceRoot, filepath.FromSlash(entry.RelPath)))
			continue
		}
		src := filepath.Join(artifactDir, "files", filepath.FromSlash(entry.RelPath))
		dst := filepath.Join(manifest.WorkspaceRoot, filepath.FromSlash(entry.RelPath))
		if err := copyFile(src, dst); err != nil {
			return err
		}
	}
	if !manifest.FullWorkspace {
		return nil
	}
	return filepath.WalkDir(manifest.WorkspaceRoot, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(manifest.WorkspaceRoot, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(filepath.Clean(rel))
		if _, ok := expected[rel]; ok {
			return nil
		}
		return os.Remove(path)
	})
}

func (s *Service) snapshotArtifactDir(record core.SnapshotRecord) (string, error) {
	if artifactDir := s.snapshots.ArtifactDir(record.ID); artifactDir != "" {
		return artifactDir, nil
	}
	artifact := strings.TrimSpace(record.Artifact)
	if artifact == "" {
		return "", fmt.Errorf("snapshot artifact directory unavailable")
	}
	return filepath.Dir(artifact), nil
}

func detectGitRepoRoot(workspaceRoot string) (string, bool) {
	out, err := runCommandOutput(workspaceRoot, "git", "rev-parse", "--show-toplevel")
	if err != nil {
		return "", false
	}
	root := strings.TrimSpace(string(out))
	if root == "" {
		return "", false
	}
	return root, true
}

func runCommandOutput(workdir string, argv ...string) ([]byte, error) {
	if len(argv) == 0 {
		return nil, fmt.Errorf("argv is required")
	}
	cmd := exec.Command(argv[0], argv[1:]...)
	if strings.TrimSpace(workdir) != "" {
		cmd.Dir = workdir
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		return out, fmt.Errorf("%s: %w", strings.Join(argv, " "), err)
	}
	return out, nil
}

func isFileMutationCall(toolName string) bool {
	switch strings.TrimSpace(toolName) {
	case "file_write", "file_replace":
		return true
	default:
		return false
	}
}

func (s *Service) attachSubagentArtifactsToLastAssistant(sessionID string, artifacts []core.SubagentArtifact) {
	if s.sessions == nil || strings.TrimSpace(sessionID) == "" || len(artifacts) == 0 {
		return
	}
	entries := s.sessions.History(sessionID)
	for i := len(entries) - 1; i >= 0; i-- {
		entry := &entries[i]
		if entry.Type != core.EntryMessage || entry.Role != core.RoleAssistant {
			continue
		}
		entry.MsgSubagentArtifacts = append(entry.MsgSubagentArtifacts, artifacts...)
		if err := s.sessions.RewriteSession(sessionID, entries); err != nil {
			logService.Warnf("attach subagent artifacts: session_id=%s error=%v", sessionID, err)
		}
		return
	}
}
