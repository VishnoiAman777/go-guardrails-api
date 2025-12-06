package policy

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/google/uuid"
	"github.com/prompt-gateway/pkg/models"
)

// Repository handles policy data access
type Repository struct {
	db *sql.DB
}

// NewRepository creates a new Repository
func NewRepository(db *sql.DB) *Repository {
	return &Repository{db: db}
}

// 1.  List returns all enabled policies
func (r *Repository) List(ctx context.Context) ([]models.Policy, error) {
	query := `
		SELECT id, name, description, pattern_type, pattern_value, 
		       severity, action, enabled, created_at, updated_at
		FROM policies
		WHERE enabled = true
		ORDER BY created_at DESC
	`

	rows, err := r.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to query policies: %w", err)
	}
	defer rows.Close()

	var policies []models.Policy
	for rows.Next() {
		var p models.Policy
		// Scan maps columns to struct fields (like Pydantic parsing)
		err := rows.Scan(
			&p.ID, &p.Name, &p.Description, &p.PatternType,
			&p.PatternValue, &p.Severity, &p.Action, &p.Enabled,
			&p.CreatedAt, &p.UpdatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan policy: %w", err)
		}
		policies = append(policies, p)
	}

	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating policies: %w", err)
	}

	return policies, nil
}

// 2. GetByID returns a policy by ID
func (r *Repository) GetByID(ctx context.Context, id uuid.UUID) (*models.Policy, error) {
	query := `
		SELECT id, name, description, pattern_type, pattern_value,
		       severity, action, enabled, created_at, updated_at
		FROM policies
		WHERE id = $1
	`

	var p models.Policy
	err := r.db.QueryRowContext(ctx, query, id).Scan(
		&p.ID, &p.Name, &p.Description, &p.PatternType,
		&p.PatternValue, &p.Severity, &p.Action, &p.Enabled,
		&p.CreatedAt, &p.UpdatedAt,
	)

	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("policy not found")
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get policy: %w", err)
	}

	return &p, nil
}

// 3. Create creates a new policy
func (r *Repository) Create(ctx context.Context, req models.CreatePolicyRequest) (*models.Policy, error) {
	// Input validation
	if err := validateCreateRequest(req); err != nil {
		return nil, err
	}

	query := `
		INSERT INTO policies (name, description, pattern_type, pattern_value, severity, action, enabled)
		VALUES ($1, $2, $3, $4, $5, $6, true)
		RETURNING id, name, description, pattern_type, pattern_value, severity, action, enabled, created_at, updated_at
	`

	var p models.Policy
	err := r.db.QueryRowContext(
		ctx, query,
		req.Name, req.Description, req.PatternType,
		req.PatternValue, req.Severity, req.Action,
	).Scan(
		&p.ID, &p.Name, &p.Description, &p.PatternType,
		&p.PatternValue, &p.Severity, &p.Action, &p.Enabled,
		&p.CreatedAt, &p.UpdatedAt,
	)

	if err != nil {
		return nil, fmt.Errorf("failed to create policy: %w", err)
	}

	return &p, nil
}

// validateCreateRequest validates the create policy request
func validateCreateRequest(req models.CreatePolicyRequest) error {
	if req.Name == "" {
		return fmt.Errorf("name is required")
	}
	validPatternTypes := map[string]bool{
		"regex":     true,
		"keyword":   true,
		"profanity": true,
		"model":     true,
	}
	if !validPatternTypes[req.PatternType] {
		return fmt.Errorf("pattern_type must be one of: regex, keyword, profanity, model")
	}
	if req.PatternValue == "" {
		return fmt.Errorf("pattern_value is required")
	}
	validSeverities := map[string]bool{"low": true, "medium": true, "high": true, "critical": true}
	if !validSeverities[req.Severity] {
		return fmt.Errorf("invalid severity: must be low, medium, high, or critical")
	}
	validActions := map[string]bool{"log": true, "block": true, "redact": true}
	if !validActions[req.Action] {
		return fmt.Errorf("invalid action: must be log, block, or redact")
	}
	return nil
}
