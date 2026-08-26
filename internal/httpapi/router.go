// Package httpapi 提供 HTTP 层：路由前缀 /api，JSON 输入输出。
package httpapi

import (
	"encoding/json"
	"log"
	"net/http"
	"runtime"
	"strconv"

	"task265-atomjump/internal/service"
)

// Server HTTP 服务器。
type Server struct {
	svc *service.Service
	mux *http.ServeMux
}

// New 构造 HTTP 服务器并注册全部路由。
func New(svc *service.Service) *Server {
	s := &Server{svc: svc, mux: http.NewServeMux()}
	s.routes()
	return s
}

// Handler 返回 http.Handler（供 main 使用）。
func (s *Server) Handler() http.Handler {
	return logMiddleware(s.mux)
}

func (s *Server) routes() {
	// 时钟
	s.mux.HandleFunc("POST /api/clocks", s.handleCreateClock)
	s.mux.HandleFunc("GET /api/clocks", s.handleListClocks)
	s.mux.HandleFunc("GET /api/clocks/{id}", s.handleGetClock)
	s.mux.HandleFunc("PATCH /api/clocks/{id}/status", s.handleClockStatus)
	s.mux.HandleFunc("POST /api/clocks/{id}/seal", s.handleSealClock)
	// 样本与环境
	s.mux.HandleFunc("POST /api/clocks/{id}/samples", s.handleIngestSample)
	s.mux.HandleFunc("GET /api/clocks/{id}/samples", s.handleListSamples)
	s.mux.HandleFunc("POST /api/clocks/{id}/env", s.handleIngestEnv)
	s.mux.HandleFunc("GET /api/clocks/{id}/env", s.handleListEnv)
	// 比对链路
	s.mux.HandleFunc("POST /api/clocks/{id}/links", s.handleCreateLink)
	s.mux.HandleFunc("GET /api/clocks/{id}/links", s.handleListLinks)
	s.mux.HandleFunc("POST /api/links/{id}/isolate", s.handleIsolateLink)
	s.mux.HandleFunc("POST /api/links/{id}/restore", s.handleRestoreLink)
	// 窗口 / 跳变 / 判别
	s.mux.HandleFunc("POST /api/clocks/{id}/analyze", s.handleAnalyze)
	s.mux.HandleFunc("GET /api/clocks/{id}/windows", s.handleListWindows)
	s.mux.HandleFunc("GET /api/clocks/{id}/jumps", s.handleListJumps)
	s.mux.HandleFunc("GET /api/jumps/{id}", s.handleGetJump)
	s.mux.HandleFunc("GET /api/jumps/{id}/candidates", s.handleJumpCandidates)
	s.mux.HandleFunc("POST /api/jumps/{id}/confirm", s.handleConfirmJump)
	// 快照
	s.mux.HandleFunc("POST /api/snapshots", s.handleCreateDraft)
	s.mux.HandleFunc("POST /api/snapshots/{id}/publish", s.handlePublishSnapshot)
	s.mux.HandleFunc("GET /api/snapshots", s.handleListSnapshots)
	s.mux.HandleFunc("GET /api/snapshots/{id}", s.handleGetSnapshot)
	// 自检 / 统计
	s.mux.HandleFunc("GET /api/health", s.handleHealth)
	s.mux.HandleFunc("GET /api/stats", s.handleStats)
	s.mux.HandleFunc("GET /api/selfcheck", s.handleSelfCheck)
}

// writeJSON 写 JSON 响应。
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Printf("write json: %v", err)
	}
}

// writeErr 写错误响应。
func writeErr(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

// parseID 解析路径参数为 int64。
func parseID(r *http.Request, name string) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue(name), 10, 64)
	if err != nil {
		return 0, false
	}
	return id, true
}

// health 自检状态。
func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"status":  "ok",
		"service": "task265-atomjump",
		"go":      runtime.Version(),
	})
}

// handleSelfCheck 执行一次数据库可达性自检。
func (s *Server) handleSelfCheck(w http.ResponseWriter, r *http.Request) {
	stats, err := s.svc.CollectStats()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"status": "ok",
		"db":     "reachable",
		"stats":  stats,
	})
}

// handleStats 汇总统计。
func (s *Server) handleStats(w http.ResponseWriter, r *http.Request) {
	stats, err := s.svc.CollectStats()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, stats)
}

// logMiddleware 简单的请求日志。
func logMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		next.ServeHTTP(w, r)
	})
}
