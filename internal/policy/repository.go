package policy

import (
	"context"

	"github.com/google/uuid"
	"github.com/prompt-gateway/pkg/models"
)

// Repository handles policy data access
type Repository struct {
	// TODO: Add database connection
}

// NewRepository creates a new Repository
func NewRepository() *Repository {
	return &Repository{}
}

// TODO: Implement repository methods

// List returns all enabled policies
func (r *Repository) List(ctx context.Context) ([]models.Policy, error) {
	return nil, nil
}

// GetByID returns a policy by ID
func (r *Repository) GetByID(ctx context.Context, id uuid.UUID) (*models.Policy, error) {
	return nil, nil
}

// Create creates a new policy
func (r *Repository) Create(ctx context.Context, req models.CreatePolicyRequest) (*models.Policy, error) {
	return nil, nil
}
