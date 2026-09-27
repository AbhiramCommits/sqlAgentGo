package main

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/spf13/cobra"

	"sqlagent/internal/agent"
	"sqlagent/internal/config"
	"sqlagent/internal/schema"
)

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func newServeCmd() *cobra.Command {
	var (
		addr       string
		reqTimeout time.Duration
	)
	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Run the sqlagent HTTP server",
		Long: `Runs the conversion API with structured logging and Prometheus metrics:

  POST /v1/convert      {source_sql, source_dialect, max_attempts}
                        -> {target_sql, attempts, status, trace_id, tokens, violations[]}
  GET  /v1/traces/{id}  -> the JSONL trace for a conversion
  GET  /healthz         liveness
  GET  /metrics         Prometheus metrics`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
			slog.SetDefault(logger)

			cfg, err := config.Load("configs", ".")
			if err != nil {
				return err
			}

			target := buildDuckDBTarget()
			targetName := "duckdb"
			if target == nil && snowflakeAvailable() {
				sf, err := newSnowflakeTarget(cmd.Context())
				if err != nil {
					return err
				}
				target, targetName = sf, "snowflake"
			}
			if target == nil {
				logger.Warn("no verification target available: duckdb not compiled in and SNOWFLAKE_ACCOUNT unset; verify will fail closed")
			}

			sch, pg, reg, err := buildPipeline(cmd.Context(), target)
			if err != nil {
				return err
			}

			a := agent.New(buildLLMClient(cfg, ""), reg,
				agent.WithExecutors(pg, target),
				agent.WithTraceDir("traces"),
				agent.WithCaseID("serve"),
			)

			h := &apiHandler{
				agent:   a,
				schema:  sch,
				timeout: reqTimeout,
				logger:  logger,
			}
			mux := http.NewServeMux()
			mux.HandleFunc("GET /healthz", h.healthz)
			mux.Handle("GET /metrics", promhttp.Handler())
			mux.HandleFunc("POST /v1/convert", h.convert)
			mux.HandleFunc("GET /v1/traces/{id...}", h.trace)

			server := &http.Server{
				Addr:              addr,
				Handler:           withRequestLog(logger, mux),
				ReadHeaderTimeout: 10 * time.Second,
			}
			logger.Info("server starting",
				"addr", addr,
				"model", cfg.LLM.Model,
				"llm_base_url", cfg.LLM.BaseURL,
				"target", targetName,
				"request_timeout", reqTimeout.String())
			return server.ListenAndServe()
		},
	}
	cmd.Flags().StringVar(&addr, "addr", ":8080", "listen address")
	cmd.Flags().DurationVar(&reqTimeout, "timeout", 5*time.Minute, "per-request timeout")
	return cmd
}

// apiHandler carries the per-server state for the HTTP API.
type apiHandler struct {
	agent   *agent.Agent
	schema  *schema.Schema
	timeout time.Duration
	logger  *slog.Logger
}

type convertRequest struct {
	SourceSQL     string `json:"source_sql"`
	SourceDialect string `json:"source_dialect"`
	MaxAttempts   int    `json:"max_attempts"`
}

type tokenUsage struct {
	Prompt     int `json:"prompt"`
	Completion int `json:"completion"`
}

type convertResponse struct {
	TargetSQL  string     `json:"target_sql"`
	Attempts   int        `json:"attempts"`
	Status     string     `json:"status"`
	TraceID    string     `json:"trace_id"`
	Tokens     tokenUsage `json:"tokens"`
	Violations []string   `json:"violations"`
	Error      string     `json:"error,omitempty"`
}

func (h *apiHandler) healthz(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// convert runs one conversion through the agent loop. Request context
// cancellation and the per-request timeout are honored throughout.
func (h *apiHandler) convert(w http.ResponseWriter, r *http.Request) {
	var req convertRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if strings.TrimSpace(req.SourceSQL) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "source_sql is required"})
		return
	}
	if req.SourceDialect == "" {
		req.SourceDialect = "tsql"
	}

	ctx, cancel := context.WithTimeout(r.Context(), h.timeout)
	defer cancel()

	st := &agent.State{
		CaseID:        "serve",
		SourceSQL:     req.SourceSQL,
		SourceDialect: req.SourceDialect,
		Schema:        *h.schema,
		MaxAttempts:   req.MaxAttempts,
	}
	res, err := h.agent.Run(ctx, st)

	resp := convertResponse{
		TargetSQL:  st.Candidate,
		Attempts:   st.Attempt,
		Status:     st.Status,
		Tokens:     tokenUsage{},
		Violations: guardViolationStrings(st),
	}
	if err != nil {
		resp.Error = err.Error()
		resp.Status = "llm_error"
	}
	if res != nil {
		resp.TraceID = strings.TrimPrefix(res.TracePath, "traces/")
		resp.Tokens = tokenUsage{Prompt: res.PromptTokens, Completion: res.CompletionTokens}
	}
	w.Header().Set("X-Trace-ID", resp.TraceID)

	status := http.StatusOK
	if resp.Status == "llm_error" {
		status = http.StatusBadGateway
	}
	writeJSON(w, status, resp)
}

// trace serves the JSONL trace for a previous conversion.
func (h *apiHandler) trace(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !validTraceID(id) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid trace id"})
		return
	}
	path := filepath.Join("traces", filepath.FromSlash(id))
	f, err := os.Open(path)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "trace not found"})
		return
	}
	defer func() { _ = f.Close() }()
	w.Header().Set("Content-Type", "application/x-ndjson")
	_, _ = io.Copy(w, f)
}

// guardViolationStrings extracts the readable guard violations recorded
// during the run (empty on green runs).
func guardViolationStrings(st *agent.State) []string {
	var out []string
	for _, s := range st.Trace {
		if s.Node != agent.NodeGuard || s.Violations == 0 || s.Detail == "" {
			continue
		}
		out = append(out, strings.Split(s.Detail, "; ")...)
	}
	return out
}

func validTraceID(id string) bool {
	if id == "" || strings.Contains(id, "..") || strings.HasPrefix(id, "/") {
		return false
	}
	return filepath.Clean(filepath.FromSlash(id)) == id
}

// statusWriter captures the response status for request logging.
type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(status int) {
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}

// withRequestLog emits one structured JSON line per request, including the
// trace id of the conversion when present.
func withRequestLog(logger *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		sw := &statusWriter{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(sw, r)
		logger.Info("request",
			"method", r.Method,
			"path", r.URL.Path,
			"status", sw.status,
			"duration_ms", time.Since(start).Milliseconds(),
			"trace_id", sw.Header().Get("X-Trace-ID"),
			"remote", r.RemoteAddr)
	})
}
