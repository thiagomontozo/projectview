package handlers

import (
	"net/http"

	"github.com/google/uuid"

	"projectview/internal/httpx"
)

// GET /api/dashboard/overview
func (a *API) Overview(w http.ResponseWriter, r *http.Request) {
	overview, err := a.Dashboard.Counters(r.Context())
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	httpx.JSON(w, http.StatusOK, overview)
}

// GET /api/dashboard/status-breakdown?projectId=
func (a *API) StatusBreakdown(w http.ResponseWriter, r *http.Request) {
	var projectID *uuid.UUID
	if raw := r.URL.Query().Get("projectId"); raw != "" {
		id, err := uuid.Parse(raw)
		if err != nil {
			httpx.Error(w, http.StatusBadRequest, "Invalid projectId.")
			return
		}
		projectID = &id
	}
	rows, err := a.Dashboard.StatusBreakdown(r.Context(), projectID)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	httpx.JSON(w, http.StatusOK, rows)
}

// GET /api/dashboard/workload-chart
func (a *API) WorkloadChart(w http.ResponseWriter, r *http.Request) {
	rows, err := a.Dashboard.WorkloadChart(r.Context())
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	httpx.JSON(w, http.StatusOK, rows)
}

type projectProgressResponse struct {
	Project projectRefLite `json:"project"`
	Total   int            `json:"total"`
	Done    int            `json:"done"`
	Percent int            `json:"percent"`
}

// GET /api/dashboard/project-progress
func (a *API) ProjectProgress(w http.ResponseWriter, r *http.Request) {
	rows, err := a.Dashboard.ProjectProgress(r.Context())
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	out := make([]projectProgressResponse, 0, len(rows))
	for _, row := range rows {
		out = append(out, projectProgressResponse{
			Project: projectRefLite{ID: row.ProjectID, Name: row.Name, Key: row.Key, Color: row.Color},
			Total:   row.Total, Done: row.Done, Percent: row.Percent,
		})
	}
	httpx.JSON(w, http.StatusOK, out)
}

// GET /api/dashboard/completion-trend - tasks completed per day, last 30 days.
func (a *API) CompletionTrend(w http.ResponseWriter, r *http.Request) {
	rows, err := a.Dashboard.CompletionTrend(r.Context())
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	httpx.JSON(w, http.StatusOK, rows)
}
