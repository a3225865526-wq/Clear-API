package authz

import (
	"fmt"

	"github.com/QuantumNous/new-api/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// builtInDefaultPolicies returns the canonical unscoped allow policies that the
// built-in non-superuser roles must hold. It is the single desired-state source
// shared by the startup reconciliation and the enforcer seeding, so a rule can
// never be deleted as stale and then not re-added.
func builtInDefaultPolicies() []model.CasbinRule {
	rules := make([]model.CasbinRule, 0)
	for _, spec := range builtInRoles {
		if spec.Superuser {
			continue
		}
		for _, permission := range PermissionsForRole(spec.Key) {
			rules = append(rules, newRule("p", []string{
				RoleSubject(spec.Key), permission.Resource, permission.Action, EffectAllow,
			}))
		}
	}
	return rules
}

func seedBuiltInRoles(db *gorm.DB) error {
	for _, spec := range builtInRoles {
		var existing model.AuthzRole
		// Map form lets GORM quote the reserved column name `key` per dialect;
		// a raw "key = ?" string is emitted verbatim and MySQL rejects it.
		found := db.Where(map[string]any{"key": spec.Key}).Limit(1).Find(&existing)
		if found.Error != nil {
			return found.Error
		}
		if found.RowsAffected == 0 {
			role := model.AuthzRole{
				Key:         spec.Key,
				Name:        spec.Name,
				Description: spec.Description,
				BuiltIn:     spec.BuiltIn,
				Enabled:     true,
				Sort:        spec.Sort,
			}
			// DoNothing lets concurrent masters race on a fresh database without a
			// unique-key error; the winner's row already carries the spec values.
			// Reading first keeps a steady-state restart from issuing an INSERT,
			// which is what advanced the autoincrement sequence on every start.
			if err := db.Clauses(clause.OnConflict{
				Columns:   []clause.Column{{Name: "key"}},
				DoNothing: true,
			}).Create(&role).Error; err != nil {
				return err
			}
			continue
		}
		if existing.Name == spec.Name && existing.Description == spec.Description &&
			existing.BuiltIn == spec.BuiltIn && existing.Enabled && existing.Sort == spec.Sort {
			continue
		}
		if err := db.Model(&existing).Updates(map[string]any{
			"name":        spec.Name,
			"description": spec.Description,
			"built_in":    spec.BuiltIn,
			"enabled":     true,
			"sort":        spec.Sort,
		}).Error; err != nil {
			return err
		}
	}
	return nil
}

func reconcileBuiltInRolePolicies(db *gorm.DB) error {
	subjects := make([]string, 0, len(builtInRoles))
	for _, spec := range builtInRoles {
		subjects = append(subjects, RoleSubject(spec.Key))
	}
	desired := make(map[model.CasbinRule]struct{})
	for _, rule := range builtInDefaultPolicies() {
		desired[rule] = struct{}{}
	}
	// Scoped legacy rules must survive reconciliation so the adapter can retain
	// their restrictions instead of replacing them with global grants; only
	// unscoped built-in rules are candidates for removal.
	var existing []model.CasbinRule
	if err := db.Where("ptype = ? AND v0 IN ?", "p", subjects).
		Where("(v4 = ? OR v4 IS NULL) AND (v5 = ? OR v5 IS NULL)", "", "").
		Find(&existing).Error; err != nil {
		return err
	}
	stale := make([]uint, 0, len(existing))
	for _, rule := range existing {
		key := rule
		key.Id = 0 // identity is the unique-key content, not the surrogate row id
		if _, ok := desired[key]; !ok {
			stale = append(stale, rule.Id)
		}
	}
	if len(stale) == 0 {
		// Identical defaults keep their rows and IDs, so a restart neither churns
		// casbin_rule nor advances its autoincrement sequence.
		return nil
	}
	return db.Where("id IN ?", stale).Delete(&model.CasbinRule{}).Error
}

func seedDefaultPolicies() error {
	e := currentEnforcer()
	if e == nil {
		return fmt.Errorf("authz enforcer is not initialized")
	}

	for _, rule := range builtInDefaultPolicies() {
		if _, err := e.AddPolicy(rule.V0, rule.V1, rule.V2, rule.V3); err != nil {
			return err
		}
	}
	return nil
}
