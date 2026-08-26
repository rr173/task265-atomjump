package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"task265-atomjump/internal/baseline"
	"task265-atomjump/internal/model"
	"task265-atomjump/internal/sampling"
)

// decodeJSON 解码请求体。
func decodeJSON(r *http.Request, v any) error {
	defer r.Body.Close()
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	return dec.Decode(v)
}

// writeStoreErr 把领域错误映射为 HTTP 状态码。
func writeStoreErr(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, model.ErrClockNotFound),
		errors.Is(err, model.ErrLinkNotFound),
		errors.Is(err, model.ErrWindowNotFound),
		errors.Is(err, model.ErrJumpNotFound),
		errors.Is(err, model.ErrSnapshotNotFound):
		writeErr(w, http.StatusNotFound, err.Error())
	case errors.Is(err, model.ErrClockSealed),
		errors.Is(err, model.ErrSnapshotImmutable),
		errors.Is(err, model.ErrSnapshotAlreadyPub):
		writeErr(w, http.StatusConflict, err.Error())
	case errors.Is(err, model.ErrCanceled):
		writeErr(w, http.StatusRequestTimeout, err.Error())
	case errors.Is(err, model.ErrInvalidTransition),
		errors.Is(err, model.ErrSampleDuplicate),
		errors.Is(err, model.ErrUnitMismatch),
		errors.Is(err, model.ErrUnknownBaseline),
		errors.Is(err, model.ErrReferenceSelfLoop),
		errors.Is(err, model.ErrInsufficientSamples),
		errors.Is(err, baseline.ErrUnitMismatch),
		errors.Is(err, baseline.ErrUnknownBaseline):
		writeErr(w, http.StatusBadRequest, err.Error())
	default:
		writeErr(w, http.StatusInternalServerError, err.Error())
	}
}

// ingestSampleRequest 样本上报请求。
type ingestSampleRequest struct {
	Seq      int64     `json:"seq"`
	TakenAt  time.Time `json:"taken_at"`
	FreqHz   float64   `json:"freq_hz"`
	Baseline string    `json:"baseline"`
}

// handleIngestSample POST /api/clocks/{id}/samples
func (s *Server) handleIngestSample(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(r, "id")
	if !ok {
		writeErr(w, http.StatusBadRequest, "invalid clock id")
		return
	}
	var req ingestSampleRequest
	if err := decodeJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if req.TakenAt.IsZero() {
		req.TakenAt = time.Now().UTC()
	}
	sm, err := s.svc.IngestSample(r.Context(), sampling.IngestInput{
		ClockID:  id,
		Seq:      req.Seq,
		TakenAt:  req.TakenAt,
		FreqHz:   req.FreqHz,
		Baseline: req.Baseline,
	})
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, sm)
}

// handleListSamples GET /api/clocks/{id}/samples?limit=N
func (s *Server) handleListSamples(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(r, "id")
	if !ok {
		writeErr(w, http.StatusBadRequest, "invalid clock id")
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	samples, err := s.svc.Store.Samples.ListByClock(id, limit)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"samples": samples, "count": len(samples)})
}

// handleIngestEnv POST /api/clocks/{id}/env
func (s *Server) handleIngestEnv(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(r, "id")
	if !ok {
		writeErr(w, http.StatusBadRequest, "invalid clock id")
		return
	}
	var env model.EnvReading
	if err := decodeJSON(r, &env); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	env.ClockID = id
	saved, err := s.svc.Receive.IngestEnv(&env)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, saved)
}

// handleListEnv GET /api/clocks/{id}/env?limit=N
func (s *Server) handleListEnv(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(r, "id")
	if !ok {
		writeErr(w, http.StatusBadRequest, "invalid clock id")
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	envs, err := s.svc.Store.Envs.ListByClock(id, limit)
	if err != nil {
		writeStoreErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"env_readings": envs, "count": len(envs)})
}
