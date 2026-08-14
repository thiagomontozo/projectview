package models

import (
	"time"

	"github.com/google/uuid"
)

type Team struct {
	ID          uuid.UUID   `json:"id"`
	Name        string      `json:"name"`
	Description string      `json:"description,omitempty"`
	Color       string      `json:"color"`
	Members     []uuid.UUID `json:"-"`
	LeadID      *uuid.UUID  `json:"-"`
	CreatedBy   *uuid.UUID  `json:"-"`
	CreatedAt   time.Time   `json:"createdAt"`
	UpdatedAt   time.Time   `json:"updatedAt"`
}
