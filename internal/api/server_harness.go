package api

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"github.com/doeshing/nekoclaw/internal/core"
)

func (s *Server) handlePlaybooks(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		statuses := make([]core.PlaybookStatus, 0, 3)
		if raw := strings.TrimSpace(r.URL.Query().Get("status")); raw != "" {
			for _, item := range strings.Split(raw, ",") {
				statuses = append(statuses, core.PlaybookStatus(strings.TrimSpace(item)))
			}
		}
		respondJSON(w, http.StatusOK, map[string]any{"entries": s.svc.ListPlaybooks(statuses...)})
	case http.MethodPost:
		var entry core.PlaybookEntry
		if err := json.NewDecoder(r.Body).Decode(&entry); err != nil {
			respondError(w, http.StatusBadRequest, "invalid request body")
			return
		}
		saved, err := s.svc.SavePlaybook(entry)
		if err != nil {
			respondError(w, http.StatusBadRequest, err.Error())
			return
		}
		respondJSON(w, http.StatusOK, saved)
	default:
		respondError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Server) handlePlaybookRoute(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/v1/playbooks/")
	path = strings.Trim(path, "/")
	if path == "" {
		respondError(w, http.StatusNotFound, "playbook not found")
		return
	}
	parts := strings.Split(path, "/")
	id := strings.TrimSpace(parts[0])
	if len(parts) == 1 && r.Method == http.MethodGet {
		entry, err := s.svc.GetPlaybook(id)
		if err != nil {
			respondError(w, http.StatusNotFound, err.Error())
			return
		}
		respondJSON(w, http.StatusOK, entry)
		return
	}
	if len(parts) == 2 && r.Method == http.MethodPost {
		switch parts[1] {
		case "approve":
			entry, err := s.svc.ApprovePlaybook(id)
			if err != nil {
				respondError(w, http.StatusBadRequest, err.Error())
				return
			}
			respondJSON(w, http.StatusOK, entry)
			return
		case "archive":
			entry, err := s.svc.ArchivePlaybook(id)
			if err != nil {
				respondError(w, http.StatusBadRequest, err.Error())
				return
			}
			respondJSON(w, http.StatusOK, entry)
			return
		}
	}
	respondError(w, http.StatusMethodNotAllowed, "method not allowed")
}

func (s *Server) handleTasks(w http.ResponseWriter, r *http.Request) {
	sessionID := strings.TrimSpace(r.URL.Query().Get("session_id"))
	switch r.Method {
	case http.MethodGet:
		if sessionID == "" {
			sessionID = "main"
		}
		respondJSON(w, http.StatusOK, s.svc.TaskList(sessionID))
	case http.MethodPut:
		var list core.TaskList
		if err := json.NewDecoder(r.Body).Decode(&list); err != nil {
			respondError(w, http.StatusBadRequest, "invalid request body")
			return
		}
		if list.SessionID == "" {
			list.SessionID = sessionID
		}
		saved, err := s.svc.SaveTaskList(list)
		if err != nil {
			respondError(w, http.StatusBadRequest, err.Error())
			return
		}
		respondJSON(w, http.StatusOK, saved)
	default:
		respondError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Server) handlePermissions(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		respondJSON(w, http.StatusOK, map[string]any{"grants": s.svc.ListPermissions()})
	case http.MethodPut:
		var payload struct {
			Grants []core.PermissionGrant `json:"grants"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			respondError(w, http.StatusBadRequest, "invalid request body")
			return
		}
		if err := s.svc.SavePermissions(payload.Grants); err != nil {
			respondError(w, http.StatusBadRequest, err.Error())
			return
		}
		respondJSON(w, http.StatusOK, map[string]any{"grants": s.svc.ListPermissions()})
	default:
		respondError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Server) handleAutomations(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		respondJSON(w, http.StatusOK, map[string]any{"automations": s.svc.ListWorkflows()})
	case http.MethodPost:
		var record core.WorkflowRecord
		if err := json.NewDecoder(r.Body).Decode(&record); err != nil {
			respondError(w, http.StatusBadRequest, "invalid request body")
			return
		}
		saved, err := s.svc.SaveWorkflow(record)
		if err != nil {
			respondError(w, http.StatusBadRequest, err.Error())
			return
		}
		respondJSON(w, http.StatusOK, saved)
	default:
		respondError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Server) handleAutomationRoute(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/v1/automations/")
	path = strings.Trim(path, "/")
	if path == "" {
		respondError(w, http.StatusNotFound, "automation not found")
		return
	}
	parts := strings.Split(path, "/")
	id := strings.TrimSpace(parts[0])
	if len(parts) == 1 {
		switch r.Method {
		case http.MethodGet:
			record, err := s.svc.GetWorkflow(id)
			if err != nil {
				respondError(w, http.StatusNotFound, err.Error())
				return
			}
			respondJSON(w, http.StatusOK, record)
			return
		case http.MethodPut:
			var record core.WorkflowRecord
			if err := json.NewDecoder(r.Body).Decode(&record); err != nil {
				respondError(w, http.StatusBadRequest, "invalid request body")
				return
			}
			record.ID = id
			saved, err := s.svc.SaveWorkflow(record)
			if err != nil {
				respondError(w, http.StatusBadRequest, err.Error())
				return
			}
			respondJSON(w, http.StatusOK, saved)
			return
		case http.MethodDelete:
			if err := s.svc.DeleteWorkflow(id); err != nil {
				respondError(w, http.StatusBadRequest, err.Error())
				return
			}
			respondJSON(w, http.StatusOK, map[string]any{"ok": true})
			return
		}
	}
	if len(parts) == 2 && parts[1] == "runs" && r.Method == http.MethodPost {
		var payload struct {
			Payload json.RawMessage `json:"payload"`
		}
		_ = json.NewDecoder(r.Body).Decode(&payload)
		run, err := s.svc.RunWorkflow(r.Context(), id, payload.Payload)
		if err != nil {
			respondError(w, http.StatusBadRequest, err.Error())
			return
		}
		respondJSON(w, http.StatusOK, run)
		return
	}
	respondError(w, http.StatusMethodNotAllowed, "method not allowed")
}

func (s *Server) handleAutomationWebhook(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		respondError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	token := strings.Trim(strings.TrimPrefix(r.URL.Path, "/v1/automations/webhooks/"), "/")
	if token == "" {
		respondError(w, http.StatusBadRequest, "missing webhook token")
		return
	}
	var matched *core.WorkflowRecord
	for _, workflow := range s.svc.ListWorkflows() {
		if workflow.TriggerKind == core.WorkflowTriggerWebhook && workflow.WebhookToken == token && workflow.Enabled {
			copy := workflow
			matched = &copy
			break
		}
	}
	if matched == nil {
		respondError(w, http.StatusNotFound, "webhook not found")
		return
	}
	body, _ := io.ReadAll(r.Body)
	run, err := s.svc.RunWorkflow(r.Context(), matched.ID, json.RawMessage(body))
	if err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}
	respondJSON(w, http.StatusOK, run)
}

func (s *Server) handleSnapshots(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		respondError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	sessionID := strings.TrimSpace(r.URL.Query().Get("session_id"))
	respondJSON(w, http.StatusOK, map[string]any{"snapshots": s.svc.ListSnapshots(sessionID)})
}

func (s *Server) handleSnapshotRoute(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		respondError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	path := strings.Trim(strings.TrimPrefix(r.URL.Path, "/v1/snapshots/"), "/")
	parts := strings.Split(path, "/")
	if len(parts) != 2 || parts[1] != "undo" {
		respondError(w, http.StatusNotFound, "snapshot route not found")
		return
	}
	id := strings.TrimSpace(parts[0])
	record, restored, message, err := s.svc.UndoSnapshot(id)
	if err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}
	respondJSON(w, http.StatusOK, map[string]any{
		"snapshot": record,
		"restored": restored,
		"message":  message,
	})
}

func (s *Server) handleTraces(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		respondError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	sessionID := strings.TrimSpace(r.URL.Query().Get("session_id"))
	respondJSON(w, http.StatusOK, map[string]any{"traces": s.svc.ListTraces(sessionID)})
}
