package handlers

import (
	"encoding/csv"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"projectview/internal/auth"
	"projectview/internal/httpx"
)

// CSV rather than XLSX or PDF.
//
// CSV opens in every spreadsheet, needs no dependency, and streams. XLSX would
// mean a library and a format nobody can diff; PDF would mean shipping a
// rendering engine to solve a problem the browser's own print dialog already
// solves for the pages that matter. What the reports genuinely need is the
// numbers somewhere they can be pivoted, and that is this.

// writeCSV streams rows with the headers a download needs.
func writeCSV(w http.ResponseWriter, filename string, header []string, rows [][]string) {
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, filename))
	// Spreadsheet applications on Windows read a bare UTF-8 CSV as the local
	// codepage; the byte-order mark is what makes accented names survive the
	// round trip.
	_, _ = w.Write([]byte{0xEF, 0xBB, 0xBF})

	out := csv.NewWriter(w)
	defer out.Flush()
	_ = out.Write(header)
	for _, row := range rows {
		_ = out.Write(row)
	}
}

func formatTime(t *time.Time) string {
	if t == nil {
		return ""
	}
	return t.Format("2006-01-02")
}

func formatFloat(v float64) string {
	return strconv.FormatFloat(v, 'f', 2, 64)
}

// GET /api/projects/:id/export.csv
func (a *API) ExportProjectTasks(w http.ResponseWriter, r *http.Request) {
	projectID, ok := httpx.UUIDParam(w, r, "id")
	if !ok {
		return
	}
	project, ok := a.requireProjectWork(w, r, projectID)
	if !ok {
		return
	}

	tasks, err := a.Tasks.ByProject(r.Context(), project.ID)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}

	// Assignee names, not ids: a spreadsheet full of UUIDs is an export nobody
	// can read without the application that produced it.
	people := map[uuid.UUID]string{}
	ids := []uuid.UUID{}
	for _, task := range tasks {
		ids = append(ids, task.Assignees...)
	}
	if resolved, err := a.Users.PublicByIDs(r.Context(), uniqueIDs(ids)); err == nil {
		for id, user := range resolved {
			people[id] = user.Name
		}
	}

	rows := make([][]string, 0, len(tasks))
	for _, task := range tasks {
		rows = append(rows, []string{
			task.ID.String(),
			task.Title,
			task.Status,
			task.Priority,
			formatTime(task.StartDate),
			formatTime(task.DueDate),
			formatTime(task.CompletedAt),
			formatFloat(task.EstimateHours),
			namesOf(task.Assignees, people),
		})
	}

	writeCSV(w, project.Key+"-tasks.csv",
		[]string{"id", "title", "status", "priority", "start", "due", "completed", "estimateHours", "assignees"},
		rows)
}

// GET /api/portfolio/export.csv
func (a *API) ExportPortfolio(w http.ResponseWriter, r *http.Request) {
	if !canAdministerStructure(auth.CurrentUser(r)) {
		httpx.Error(w, http.StatusForbidden, forbiddenMessage)
		return
	}

	projects, err := a.Portfolio.Summary(r.Context(), nil, time.Now())
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}

	rows := make([][]string, 0, len(projects))
	for _, p := range projects {
		rows = append(rows, []string{
			p.Key, p.Name, p.Status, p.Health,
			strconv.Itoa(p.TotalTasks), strconv.Itoa(p.DoneTasks), strconv.Itoa(p.OverdueOpen),
			formatFloat(p.Progress * 100),
			formatFloat(p.Estimated), formatFloat(p.Tracked),
			formatTime(p.StartDate), formatTime(p.EndDate),
		})
	}

	writeCSV(w, "portfolio.csv",
		[]string{"key", "name", "status", "health", "tasks", "done", "overdue",
			"progressPercent", "estimatedHours", "trackedHours", "start", "end"},
		rows)
}

// GET /api/portfolio/capacity/export.csv
func (a *API) ExportCapacity(w http.ResponseWriter, r *http.Request) {
	if !canAdministerStructure(auth.CurrentUser(r)) {
		httpx.Error(w, http.StatusForbidden, forbiddenMessage)
		return
	}

	from, to := parseWindow(r, 4*7*24*time.Hour)
	capacity, err := a.Portfolio.Capacity(r.Context(), from, to)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}

	rows := make([][]string, 0, len(capacity))
	for _, row := range capacity {
		rows = append(rows, []string{
			row.Name, row.Email,
			formatFloat(row.Capacity), formatFloat(row.Committed),
			formatFloat(row.Utilisation * 100),
			strconv.Itoa(row.OpenTasks), strconv.Itoa(row.Projects),
		})
	}

	writeCSV(w, "capacity.csv",
		[]string{"name", "email", "capacityHours", "committedHours",
			"utilisationPercent", "openTasks", "projects"},
		rows)
}

// namesOf renders an assignee list for a spreadsheet cell.
func namesOf(ids []uuid.UUID, names map[uuid.UUID]string) string {
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		if name, ok := names[id]; ok {
			out = append(out, name)
		}
	}
	return strings.Join(out, ", ")
}
