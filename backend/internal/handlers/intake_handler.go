package handlers

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"projectview/internal/audit"
	"projectview/internal/auth"
	"projectview/internal/httpx"
	"projectview/internal/models"
	"projectview/internal/repo"
)

// Intake forms.
//
// Two audiences and therefore two route groups. Managing a form is project
// work; submitting one may be anonymous, and that half deliberately lives
// outside every authenticated route so it cannot inherit a permission it
// should not have.

type intakeFormRequest struct {
	Title       string             `json:"title"`
	Description string             `json:"description"`
	Fields      []repo.IntakeField `json:"fields"`
	Status      string             `json:"targetStatus"`
	Priority    string             `json:"targetPriority"`
	Public      bool               `json:"public"`
}

// GET /api/projects/:projectId/intake
func (a *API) ListIntakeForms(w http.ResponseWriter, r *http.Request) {
	projectID, ok := httpx.UUIDParam(w, r, "projectId")
	if !ok {
		return
	}
	if _, ok := a.requireProjectWork(w, r, projectID); !ok {
		return
	}
	forms, err := a.Intake.ForProject(r.Context(), projectID)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	httpx.JSON(w, http.StatusOK, forms)
}

// POST /api/projects/:projectId/intake
//
// Creating a form decides what work arrives and where it lands, so it takes the
// permission to reconfigure the project rather than merely to work in it.
func (a *API) CreateIntakeForm(w http.ResponseWriter, r *http.Request) {
	projectID, ok := httpx.UUIDParam(w, r, "projectId")
	if !ok {
		return
	}
	project, ok := a.requireProjectManage(w, r, projectID)
	if !ok {
		return
	}

	var req intakeFormRequest
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	if strings.TrimSpace(req.Title) == "" {
		httpx.Error(w, http.StatusBadRequest, "A form needs a title.")
		return
	}

	seen := map[string]bool{}
	for i, field := range req.Fields {
		if strings.TrimSpace(field.Key) == "" || strings.TrimSpace(field.Label) == "" {
			httpx.Error(w, http.StatusBadRequest, fmt.Sprintf("Field %d needs a key and a label.", i+1))
			return
		}
		if !repo.ValidIntakeFieldType(field.Type) {
			httpx.Error(w, http.StatusBadRequest, "Unsupported field type: "+field.Type)
			return
		}
		// Duplicate keys would overwrite each other in the answers, so one of
		// the two questions would vanish without anybody noticing.
		if seen[field.Key] {
			httpx.Error(w, http.StatusBadRequest, "Two fields share the key "+field.Key+".")
			return
		}
		seen[field.Key] = true
	}

	if req.Priority == "" {
		req.Priority = models.PriorityMedium
	}
	if !models.ValidPriority(req.Priority) {
		httpx.Error(w, http.StatusBadRequest, "Invalid priority.")
		return
	}
	if req.Status == "" && len(project.Statuses) > 0 {
		req.Status = project.Statuses[0].Key
	}

	slug, err := repo.NewSlug()
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}

	requester := auth.CurrentUser(r)
	form := &repo.IntakeForm{
		ProjectID: projectID, Title: req.Title, Description: req.Description,
		Fields: req.Fields, Status: req.Status, Priority: req.Priority,
		Enabled: true, Public: req.Public, Slug: slug, CreatedBy: &requester.ID,
	}
	if form.Fields == nil {
		form.Fields = []repo.IntakeField{}
	}
	if err := a.Intake.Create(r.Context(), form); err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}

	a.Audit.Record(r, requester, audit.Event{
		Action: audit.ActionIntakeFormCreated, ResourceType: "intake_form",
		ResourceID: form.ID.String(),
		Changes:    map[string]any{"title": form.Title, "public": form.Public},
		Status:     http.StatusCreated,
	})
	httpx.JSON(w, http.StatusCreated, form)
}

// PATCH /api/intake/:id - open or close a form.
func (a *API) SetIntakeFormEnabled(w http.ResponseWriter, r *http.Request) {
	form, ok := a.requireFormManage(w, r)
	if !ok {
		return
	}
	var req struct {
		Enabled bool `json:"enabled"`
	}
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	if err := a.Intake.SetEnabled(r.Context(), form.ID, req.Enabled); err != nil {
		respondRepoError(w, err, "Form not found.")
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]bool{"enabled": req.Enabled})
}

// DELETE /api/intake/:id
func (a *API) DeleteIntakeForm(w http.ResponseWriter, r *http.Request) {
	form, ok := a.requireFormManage(w, r)
	if !ok {
		return
	}
	if err := a.Intake.Delete(r.Context(), form.ID); err != nil {
		respondRepoError(w, err, "Form not found.")
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// GET /api/intake/:id/submissions - what was asked for, as it was asked.
func (a *API) IntakeSubmissions(w http.ResponseWriter, r *http.Request) {
	form, ok := a.requireFormManage(w, r)
	if !ok {
		return
	}
	submissions, err := a.Intake.Submissions(r.Context(), form.ID, 50)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	httpx.JSON(w, http.StatusOK, submissions)
}

func (a *API) requireFormManage(w http.ResponseWriter, r *http.Request) (*repo.IntakeForm, bool) {
	id, ok := httpx.UUIDParam(w, r, "id")
	if !ok {
		return nil, false
	}
	form, err := a.Intake.ByID(r.Context(), id)
	if err != nil {
		respondRepoError(w, err, "Form not found.")
		return nil, false
	}
	if _, ok := a.requireProjectManage(w, r, form.ProjectID); !ok {
		return nil, false
	}
	return form, true
}

// ---------------------------------------------------------------------------
// The public half
// ---------------------------------------------------------------------------

type intakeSubmitRequest struct {
	Answers map[string]any `json:"answers"`
	Name    string         `json:"submitterName"`
	Email   string         `json:"submitterEmail"`
}

// GET /api/public/intake/:slug - what to render, for somebody who may not be
// signed in.
//
// Returns the questions and nothing else. Not the project, not its members, not
// the other forms on it: a public address is reachable by anyone who learns it,
// so it answers exactly what filling the form requires and no more.
func (a *API) PublicIntakeForm(w http.ResponseWriter, r *http.Request) {
	form, ok := a.resolvePublicForm(w, r)
	if !ok {
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{
		"title":       form.Title,
		"description": form.Description,
		"fields":      form.Fields,
	})
}

// POST /api/public/intake/:slug - submit, and get a task out of it.
func (a *API) SubmitIntakeForm(w http.ResponseWriter, r *http.Request) {
	form, ok := a.resolvePublicForm(w, r)
	if !ok {
		return
	}

	var req intakeSubmitRequest
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	if req.Answers == nil {
		req.Answers = map[string]any{}
	}

	// Required fields are checked here rather than trusted from the browser:
	// this endpoint is reachable by anything that can make a request.
	for _, field := range form.Fields {
		if !field.Required {
			continue
		}
		value, present := req.Answers[field.Key]
		if !present || value == nil || strings.TrimSpace(fmt.Sprint(value)) == "" {
			httpx.Error(w, http.StatusBadRequest, field.Label+" is required.")
			return
		}
	}

	// Answers to questions the form does not ask are dropped rather than
	// stored: otherwise "what was submitted" is partly whatever the sender
	// decided to invent.
	answers := map[string]any{}
	for _, field := range form.Fields {
		if value, present := req.Answers[field.Key]; present {
			answers[field.Key] = value
		}
	}

	ctx := r.Context()
	submitter := auth.CurrentUser(r)

	task := &models.Task{
		ID: uuid.New(), ProjectID: form.ProjectID,
		Title:       intakeTitle(form, answers),
		Description: intakeDescription(form, answers, req.Name, req.Email),
		Status:      form.Status, Priority: form.Priority,
		Assignees: []uuid.UUID{}, Tags: []string{"intake"},
	}
	if task.Status == "" {
		task.Status = "todo"
	}
	if submitter != nil {
		task.CreatedBy = &submitter.ID
	}

	if err := a.Tasks.Create(ctx, task); err != nil {
		httpx.Error(w, http.StatusInternalServerError, "The request could not be recorded.")
		return
	}

	submission := &repo.IntakeSubmission{
		FormID: form.ID, TaskID: &task.ID, Answers: answers,
		Name: strings.TrimSpace(req.Name), Email: strings.TrimSpace(req.Email),
	}
	if submitter != nil {
		submission.SubmittedBy = &submitter.ID
	}
	if err := a.Intake.RecordSubmission(ctx, submission); err != nil {
		// The task exists, which is what the submitter cares about. Losing the
		// verbatim answers is worth recording, not worth a failed submission
		// that invites them to send the whole thing again.
		a.Audit.RecordAnonymous(r, submission.Email, audit.Event{
			Action: audit.ActionIntakeSubmitted, ResourceType: "intake_form",
			ResourceID: form.ID.String(),
			Changes:    map[string]any{"error": "answers not stored"},
			Status:     http.StatusInternalServerError,
		})
	}

	a.Audit.RecordAnonymous(r, submission.Email, audit.Event{
		Action: audit.ActionIntakeSubmitted, ResourceType: "intake_form",
		ResourceID: form.ID.String(),
		Changes:    map[string]any{"task": task.ID.String()},
		Status:     http.StatusCreated,
	})

	// Deliberately thin: the submitter is told it arrived, not where it landed.
	// The task id would be a handle onto a board they have no permission on.
	httpx.JSON(w, http.StatusCreated, map[string]any{"received": true})
}

// resolvePublicForm loads a form by slug, refusing a private one to anyone who
// is not signed in.
func (a *API) resolvePublicForm(w http.ResponseWriter, r *http.Request) (*repo.IntakeForm, bool) {
	slug := strings.TrimSpace(chi.URLParam(r, "slug"))
	if slug == "" {
		httpx.Error(w, http.StatusNotFound, "Form not found.")
		return nil, false
	}
	form, err := a.Intake.BySlug(r.Context(), slug)
	if err != nil {
		// A disabled form and an unknown one answer identically, so closing a
		// form does not confirm it ever existed.
		httpx.Error(w, http.StatusNotFound, "Form not found.")
		return nil, false
	}
	if !form.Public && auth.CurrentUser(r) == nil {
		httpx.Error(w, http.StatusNotFound, "Form not found.")
		return nil, false
	}
	return form, true
}

// intakeTitle names the task a submission produces.
//
// The first text answer wins, because that is what somebody wrote to describe
// their request; the form's own title is the fallback, so a task always arrives
// with a readable name rather than an empty one.
func intakeTitle(form *repo.IntakeForm, answers map[string]any) string {
	for _, field := range form.Fields {
		if field.Type != "text" {
			continue
		}
		if value, ok := answers[field.Key]; ok {
			if text := strings.TrimSpace(fmt.Sprint(value)); text != "" {
				return truncateRunes(text, 200)
			}
		}
	}
	return form.Title
}

func intakeDescription(form *repo.IntakeForm, answers map[string]any, name, email string) string {
	var b strings.Builder
	for _, field := range form.Fields {
		value, ok := answers[field.Key]
		if !ok {
			continue
		}
		fmt.Fprintf(&b, "%s: %v\n", field.Label, value)
	}
	if name != "" || email != "" {
		fmt.Fprintf(&b, "\nSubmitted by: %s %s\n", name, email)
	}
	return strings.TrimSpace(b.String())
}

func truncateRunes(s string, max int) string {
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	return string(runes[:max])
}
