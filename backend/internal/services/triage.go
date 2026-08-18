package services

import (
	"context"
	"fmt"
	"time"

	"projectview/internal/ai"
	"projectview/internal/config"
	"projectview/internal/logger"
	"projectview/internal/models"
	"projectview/internal/repo"
)

// TriageSweep asks the model about intake submissions nobody has triaged yet.
//
// A sweep rather than part of the submission request, and that is not an
// optimisation. The submission endpoint is a *public* form: making somebody
// outside the company wait eight seconds on a model before their request is
// accepted would be a bad form, and a model that is down would turn into a form
// that is down. Creating the task is the promise; the suggestion is an extra
// that arrives when it arrives.
//
// Guarded like the other sweeps, so two replicas do not each pay for the same
// completion.
type TriageSweep struct {
	Guard *SweepGuard

	cfg      *config.Config
	intake   *repo.Intake
	projects *repo.Projects
	users    *repo.Users
	interval time.Duration
	batch    int
}

func NewTriageSweep(cfg *config.Config, intake *repo.Intake, projects *repo.Projects, users *repo.Users) *TriageSweep {
	return &TriageSweep{
		cfg:      cfg,
		intake:   intake,
		projects: projects,
		users:    users,
		// Intake volume is low by nature - it is people asking for things - so
		// a minute is prompt without polling an empty table all day.
		interval: time.Minute,
		// Bounded per pass: a burst of submissions should not become one
		// enormous bill in a single tick.
		batch: 10,
	}
}

// client builds one from the settings as they stand right now.
//
// Per pass rather than once at startup, for the reason the mailer reads SMTP
// at send time: these settings are editable from the administration screen and
// are promised to apply without a restart. A client captured at boot would
// quietly ignore an administrator who turned the model on - the failure mode
// being that the screen says it is on and nothing happens.
//
// Returns nil when nothing is configured, which every method here tolerates.
func (s *TriageSweep) client() *ai.Client {
	settings := s.cfg.AI()
	if !settings.Enabled {
		return nil
	}
	client, err := ai.New(ai.Config{
		Endpoint: settings.Endpoint,
		APIKey:   settings.APIKey,
		Model:    settings.Model,
		Timeout:  time.Duration(settings.Timeout) * time.Second,
	})
	if err != nil {
		logger.Warn("Triage: the model endpoint is not usable: %v", err)
		return nil
	}
	return client
}

// Start runs the sweep whether or not a model is configured yet, because that
// answer can change while the process is running.
func (s *TriageSweep) Start() {
	if s.intake == nil {
		return
	}
	go func() {
		ticker := time.NewTicker(s.interval)
		defer ticker.Stop()
		for {
			s.Guard.Do(context.Background(), "Triage", func(ctx context.Context) { s.Run(ctx) })
			<-ticker.C
		}
	}()
}

// Run performs one pass and reports how many submissions it answered on.
func (s *TriageSweep) Run(ctx context.Context) int {
	client := s.client()
	if !client.Enabled() {
		return 0
	}

	pending, err := s.intake.AwaitingTriage(ctx, s.batch)
	if err != nil {
		logger.Warn("Triage: could not read pending submissions: %v", err)
		return 0
	}

	done := 0
	for _, submission := range pending {
		if s.triageOne(ctx, client, submission) {
			done++
		}
	}
	if done > 0 {
		logger.Info("Triage: suggested on %d submission(s)", done)
	}
	return done
}

func (s *TriageSweep) triageOne(ctx context.Context, client *ai.Client, pending repo.PendingTriage) bool {
	project, err := s.projects.ByID(ctx, pending.ProjectID)
	if err != nil {
		logger.Warn("Triage: submission %s has no project, skipping", pending.SubmissionID)
		return false
	}

	// The candidate lists are the security boundary, not the prompt. Only
	// people already on this project can be suggested, and only the four real
	// priorities - so the worst a hostile submission achieves is a wrong
	// suggestion somebody rejects.
	candidates := make([]ai.Candidate, 0, len(project.Members))
	for _, memberID := range project.Members {
		person, err := s.users.ByID(ctx, memberID)
		if err != nil || !person.Active {
			continue
		}
		candidates = append(candidates, ai.Candidate{ID: person.ID.String(), Name: person.Name})
	}

	// Answers are presented in the form's own order with their labels, so the
	// model sees the questions rather than a bag of keys.
	answers := make([][2]string, 0, len(pending.Fields))
	for _, field := range pending.Fields {
		if value, ok := pending.Answers[field.Key]; ok {
			answers = append(answers, [2]string{field.Label, toText(value)})
		}
	}

	callCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()

	suggestion, err := client.Triage(callCtx, ai.TriageInput{
		FormTitle:  pending.FormTitle,
		Answers:    answers,
		Priorities: []string{models.PriorityLow, models.PriorityMedium, models.PriorityHigh, models.PriorityUrgent},
		Assignees:  candidates,
	})
	if err != nil {
		// Left untriaged so the next pass tries again: a model that was
		// briefly unreachable should not mean a submission is never looked at.
		// A model that is permanently misconfigured produces a log line every
		// minute, which is the right amount of noise for something an
		// administrator needs to fix.
		logger.Warn("Triage: %s could not be triaged: %v", pending.SubmissionID, err)
		return false
	}

	stored := map[string]any{
		"priority":     suggestion.Priority,
		"assigneeId":   suggestion.AssigneeID,
		"assigneeName": suggestion.AssigneeName,
		"summary":      suggestion.Summary,
		"model":        suggestion.Model,
		"tokens":       suggestion.Tokens,
	}
	if err := s.intake.StoreSuggestion(ctx, pending.SubmissionID, stored); err != nil {
		logger.Error("Triage: could not store the suggestion for %s: %v", pending.SubmissionID, err)
		return false
	}
	return true
}

// toText flattens an answer for the prompt. Answers are JSONB, so a checkbox
// arrives as a bool and a number as a float; the model reads text either way.
func toText(value any) string {
	if value == nil {
		return ""
	}
	if s, ok := value.(string); ok {
		return s
	}
	return fmt.Sprint(value)
}
