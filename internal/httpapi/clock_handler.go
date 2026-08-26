package httpapi

import (
	"net/http"

	"task265-atomjump/internal/model"
)

// createClockRequest 创建时钟请求。
type createClockRequest struct {
	Name         string  `json:"name"`
	ClockType    string  `json:"clock_type"`
	NominalHz    float64 `json:"nominal_hz"`
	IsReference  bool    `json:"is_reference"`
	ReferenceID  int64   `json:"reference_id"`
}

// handleCreateClock POST /api/clocks
func (s *Server) handleCreateClock(w http.ResponseWriter, r *http.Request) {
	var req createClockRequest
	if err := decodeJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if req.Name == "" || req.NominalHz <= 0 {
		writeErr(w, http.StatusBadRequest, "name and nominal_hz are required")
		return
	}
	clk, err := s.svc.Store.Clocks.Create(req.Name, req.ClockType, req.NominalHz, req.IsReference, req.ReferenceID)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	if req.IsReference {
		clk, err = s.svc.Store.Clocks.MarkIsReference(clk.ID)
		if err != nil {
			writeStoreErr(w, err)
			return
		}
	}
	writeJSON(w, http.StatusCreated, clk)
}

// handleListClocks GET /api/clocks
func (s *Server) handleListClocks(w http.ResponseWriter, r *http.Request) {
	clocks, err := s.svc.Store.Clocks.List()
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"clocks": clocks, "count": len(clocks)})
}

// handleGetClock GET /api/clocks/{id}
func (s *Server) handleGetClock(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(r, "id")
	if !ok {
		writeErr(w, http.StatusBadRequest, "invalid clock id")
		return
	}
	clk, err := s.svc.Store.Clocks.Get(id)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, clk)
}

// clockStatusRequest 状态流转请求。
type clockStatusRequest struct {
	Status string `json:"status"`
}

// handleClockStatus PATCH /api/clocks/{id}/status
func (s *Server) handleClockStatus(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(r, "id")
	if !ok {
		writeErr(w, http.StatusBadRequest, "invalid clock id")
		return
	}
	var req clockStatusRequest
	if err := decodeJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	clk, err := s.svc.Store.Clocks.SetStatus(id, model.ClockStatus(req.Status))
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, clk)
}

// handleSealClock POST /api/clocks/{id}/seal
func (s *Server) handleSealClock(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(r, "id")
	if !ok {
		writeErr(w, http.StatusBadRequest, "invalid clock id")
		return
	}
	clk, err := s.svc.SealClock(id)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, clk)
}
