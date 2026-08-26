package httpapi

import (
	"net/http"

	"task265-atomjump/internal/model"
)

// createLinkRequest 建立比对链路请求。
type createLinkRequest struct {
	ReferenceID int64   `json:"reference_id"`
	OffsetPPB   float64 `json:"offset_ppb"`
	LatencyMS   int64   `json:"latency_ms"`
}

// handleCreateLink POST /api/clocks/{id}/links
func (s *Server) handleCreateLink(w http.ResponseWriter, r *http.Request) {
	subjectID, ok := parseID(r, "id")
	if !ok {
		writeErr(w, http.StatusBadRequest, "invalid clock id")
		return
	}
	var req createLinkRequest
	if err := decodeJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if req.ReferenceID <= 0 {
		writeErr(w, http.StatusBadRequest, "reference_id is required")
		return
	}
	// 参考钟必须存在
	if _, err := s.svc.Store.Clocks.Get(req.ReferenceID); err != nil {
		writeStoreErr(w, err)
		return
	}
	link, err := s.svc.Store.Links.Upsert(subjectID, req.ReferenceID, req.OffsetPPB, req.LatencyMS)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, link)
}

// handleListLinks GET /api/clocks/{id}/links
func (s *Server) handleListLinks(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(r, "id")
	if !ok {
		writeErr(w, http.StatusBadRequest, "invalid clock id")
		return
	}
	links, err := s.svc.Store.Links.ListBySubject(id)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"links": links, "count": len(links)})
}

// handleIsolateLink POST /api/links/{id}/isolate
func (s *Server) handleIsolateLink(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(r, "id")
	if !ok {
		writeErr(w, http.StatusBadRequest, "invalid link id")
		return
	}
	link, err := s.svc.LinkAction(id, true)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, link)
}

// handleRestoreLink POST /api/links/{id}/restore
func (s *Server) handleRestoreLink(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(r, "id")
	if !ok {
		writeErr(w, http.StatusBadRequest, "invalid link id")
		return
	}
	link, err := s.svc.LinkAction(id, false)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, link)
}

// handleAnalyze POST /api/clocks/{id}/analyze
func (s *Server) handleAnalyze(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(r, "id")
	if !ok {
		writeErr(w, http.StatusBadRequest, "invalid clock id")
		return
	}
	result, err := s.svc.AnalyzeClockCtx(r.Context(), id)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

// handleListWindows GET /api/clocks/{id}/windows
func (s *Server) handleListWindows(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(r, "id")
	if !ok {
		writeErr(w, http.StatusBadRequest, "invalid clock id")
		return
	}
	wins, err := s.svc.Store.Windows.ListByClock(id)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"windows": wins, "count": len(wins)})
}

// handleListJumps GET /api/clocks/{id}/jumps
func (s *Server) handleListJumps(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(r, "id")
	if !ok {
		writeErr(w, http.StatusBadRequest, "invalid clock id")
		return
	}
	jumps, err := s.svc.Store.Jumps.ListJumpsByClock(id)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"jumps": jumps, "count": len(jumps)})
}

// handleGetJump GET /api/jumps/{id}
func (s *Server) handleGetJump(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(r, "id")
	if !ok {
		writeErr(w, http.StatusBadRequest, "invalid jump id")
		return
	}
	jump, err := s.svc.Store.Jumps.GetJump(id)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, jump)
}

// handleJumpCandidates GET /api/jumps/{id}/candidates
func (s *Server) handleJumpCandidates(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(r, "id")
	if !ok {
		writeErr(w, http.StatusBadRequest, "invalid jump id")
		return
	}
	cands, err := s.svc.Store.Jumps.ListCandidates(id)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"candidates": cands, "count": len(cands)})
}

// confirmJumpRequest 确认来源请求。
type confirmJumpRequest struct {
	Source model.CandidateSource `json:"source"`
}

// handleConfirmJump POST /api/jumps/{id}/confirm
func (s *Server) handleConfirmJump(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(r, "id")
	if !ok {
		writeErr(w, http.StatusBadRequest, "invalid jump id")
		return
	}
	var req confirmJumpRequest
	if err := decodeJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := s.svc.ConfirmJump(id, req.Source); err != nil {
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "confirmed", "jump_id": r.PathValue("id")})
}
