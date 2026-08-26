package httpapi

import (
	"net/http"

	"task265-atomjump/internal/snapshot"
)

// draftRequest 创建诊断草稿请求。
type draftRequest struct {
	ClockID     int64  `json:"clock_id"`
	ReferenceID int64  `json:"reference_id"`
	Summary     string `json:"summary"`
}

// handleCreateDraft POST /api/snapshots
func (s *Server) handleCreateDraft(w http.ResponseWriter, r *http.Request) {
	var req draftRequest
	if err := decodeJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if req.ClockID <= 0 {
		writeErr(w, http.StatusBadRequest, "clock_id is required")
		return
	}
	if req.ReferenceID <= 0 {
		// 草稿必须显式给定参考钟，保证不可变快照固定参考配置
		writeErr(w, http.StatusBadRequest, "reference_id is required for snapshot")
		return
	}
	draft, err := s.svc.Publish.CreateDraft(snapshot.DraftInput{
		ClockID:     req.ClockID,
		ReferenceID: req.ReferenceID,
		Summary:     req.Summary,
	})
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, draft)
}

// handlePublishSnapshot POST /api/snapshots/{id}/publish
func (s *Server) handlePublishSnapshot(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(r, "id")
	if !ok {
		writeErr(w, http.StatusBadRequest, "invalid snapshot id")
		return
	}
	snap, err := s.svc.Publish.PublishCtx(r.Context(), id)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, snap)
}

// handleListSnapshots GET /api/snapshots
func (s *Server) handleListSnapshots(w http.ResponseWriter, r *http.Request) {
	snaps, err := s.svc.Store.Snapshots.List()
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"snapshots": snaps, "count": len(snaps)})
}

// handleGetSnapshot GET /api/snapshots/{id}
func (s *Server) handleGetSnapshot(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(r, "id")
	if !ok {
		writeErr(w, http.StatusBadRequest, "invalid snapshot id")
		return
	}
	snap, err := s.svc.Store.Snapshots.Get(id)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, snap)
}
