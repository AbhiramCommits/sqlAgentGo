package main

import (
	"encoding/json"
	"net/http"

	"github.com/spf13/cobra"

	"sqlagent/internal/agent"
	"sqlagent/internal/config"
)

func newServeCmd() *cobra.Command {
	var addr string
	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Run the sqlagent HTTP server",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load("configs", ".")
			if err != nil {
				return err
			}
			a, sch, err := buildAgent(cmd.Context(), cfg, "serve", 0)
			if err != nil {
				return err
			}

			mux := http.NewServeMux()
			mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
				writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
			})
			mux.HandleFunc("POST /convert", func(w http.ResponseWriter, r *http.Request) {
				var req struct {
					Dialect string `json:"dialect"`
					SQL     string `json:"sql"`
				}
				if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
					writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
					return
				}
				if req.Dialect == "" {
					req.Dialect = "tsql"
				}
				st := &agent.State{
					SourceSQL:     req.SQL,
					SourceDialect: req.Dialect,
					Schema:        *sch,
				}
				res, err := a.Run(r.Context(), st)
				if err != nil {
					writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
					return
				}
				writeJSON(w, http.StatusOK, map[string]any{
					"converted":  st.Candidate,
					"status":     st.Status,
					"attempts":   st.Attempt,
					"trace_path": res.TracePath,
				})
			})

			return http.ListenAndServe(addr, mux)
		},
	}
	cmd.Flags().StringVar(&addr, "addr", ":8080", "listen address")
	return cmd
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
