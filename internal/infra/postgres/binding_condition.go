package postgres

import "github.com/aportcullis/portcullis/internal/domain/identity"

// decodeBindingCondition refuses incomplete stored shape while preserving unsupported source for consumer validation.
func decodeBindingCondition(title, description, expression, language, environment *string) (identity.Condition, bool, error) {
	if expression == nil {
		if title != nil || description != nil || language != nil || environment != nil {
			return identity.Condition{}, false, identity.ErrInvalidCondition
		}
		return identity.Condition{}, false, nil
	}
	if title == nil || language == nil || environment == nil {
		return identity.Condition{}, false, identity.ErrInvalidCondition
	}
	source := identity.Condition{Title: *title, Expression: *expression, LanguageVersion: *language, EnvironmentVersion: *environment}
	if description != nil {
		source.Description = *description
	}
	return source, true, nil
}
