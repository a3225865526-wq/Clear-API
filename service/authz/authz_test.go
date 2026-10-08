package authz

import (
	"os"
	"sync"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func newAuthzTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	wasMaster := common.IsMasterNode
	common.IsMasterNode = true
	t.Cleanup(func() {
		common.IsMasterNode = wasMaster
	})
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	require.NoError(t, db.AutoMigrate(&model.CasbinRule{}, &model.AuthzRole{}))
	return db
}

func TestInitSeedsBuiltInRolesAndPoliciesOnce(t *testing.T) {
	db := newAuthzTestDB(t)

	require.NoError(t, Init(db))
	require.NoError(t, Init(db))

	// root is a superuser role and is granted everything implicitly, so only the
	// admin baseline is written as explicit policy rows.
	var count int64
	require.NoError(t, db.Model(&model.CasbinRule{}).Count(&count).Error)
	assert.Equal(t, int64(len(PermissionsForRole(BuiltInRoleAdmin))), count)

	var roles []model.AuthzRole
	require.NoError(t, db.Order("sort asc").Find(&roles).Error)
	require.Len(t, roles, 2)
	assert.Equal(t, BuiltInRoleRoot, roles[0].Key)
	assert.Equal(t, BuiltInRoleAdmin, roles[1].Key)

	assert.True(t, Can(1, common.RoleRootUser, ChannelSensitiveWrite))
	assert.True(t, Can(2, common.RoleAdminUser, ChannelRead))
	assert.True(t, Can(2, common.RoleAdminUser, ChannelOperate))
	assert.True(t, Can(2, common.RoleAdminUser, ChannelWrite))
	assert.False(t, Can(2, common.RoleAdminUser, ChannelSensitiveWrite))
	assert.False(t, Can(3, common.RoleCommonUser, ChannelRead))
}

func TestInitOnSlaveOnlyLoadsPolicies(t *testing.T) {
	wasMaster := common.IsMasterNode
	common.IsMasterNode = false
	t.Cleanup(func() {
		common.IsMasterNode = wasMaster
	})
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	require.NoError(t, db.AutoMigrate(&model.CasbinRule{}, &model.AuthzRole{}))

	require.NoError(t, Init(db))

	var roleCount int64
	require.NoError(t, db.Model(&model.AuthzRole{}).Count(&roleCount).Error)
	assert.Equal(t, int64(0), roleCount)
	var policyCount int64
	require.NoError(t, db.Model(&model.CasbinRule{}).Count(&policyCount).Error)
	assert.Equal(t, int64(0), policyCount)
	assert.False(t, Can(2, common.RoleAdminUser, ChannelRead))
}

func TestLegacyScopedPoliciesDoNotExpandPermissions(t *testing.T) {
	for _, master := range []bool{true, false} {
		name := "master"
		if !master {
			name = "slave"
		}
		t.Run(name, func(t *testing.T) {
			db := newAuthzTestDB(t)
			common.IsMasterNode = master
			rules := []model.CasbinRule{
				{Ptype: "p", V0: "role:vendor", V1: "channel", V2: "read", V3: "allow", V4: "own"},
				{Ptype: "p", V0: UserSubject(42), V1: "channel", V2: "read", V3: "allow", V4: "own"},
				{Ptype: "p", V0: UserSubject(43), V1: "channel", V2: "sensitive_write", V3: "allow", V4: "all"},
				{Ptype: "p", V0: UserSubject(44), V1: "channel", V2: "secret_view", V3: "deny", V4: "all"},
				{Ptype: "p", V0: UserSubject(45), V1: "channel", V2: "read"},
				{Ptype: "p", V0: UserSubject(46), V1: "channel", V2: "read", V3: "allow", V4: "unknown-scope"},
				{Ptype: "p", V0: UserSubject(47), V1: "channel", V2: "read", V3: "allow", V5: "own"},
				{Ptype: "p", V0: UserSubject(48), V1: "channel", V2: "read", V3: "allow", V4: "own"},
				{Ptype: "p", V0: UserSubject(48), V1: "channel", V2: "read", V3: "allow", V4: "all"},
				{Ptype: "p", V0: RoleSubject(BuiltInRoleAdmin), V1: "channel", V2: "operate", V3: "allow", V4: "own"},
				{Ptype: "p", V0: UserSubject(50), V1: "channel", V2: "sensitive_write", V3: "allow"},
				{Ptype: "g", V0: UserSubject(99), V1: RoleSubject(BuiltInRoleAdmin)},
			}
			require.NoError(t, db.Create(&rules).Error)
			ids := make([]uint, len(rules))
			for i := range rules {
				ids[i] = rules[i].Id
			}
			for range 2 {
				require.NoError(t, Init(db))
				require.NoError(t, ReloadPolicy())
				assert.False(t, Can(42, common.RoleAdminUser, ChannelRead), "own must not fall back to the admin allow baseline")
				assert.True(t, Can(43, common.RoleAdminUser, ChannelSensitiveWrite))
				assert.False(t, Can(44, common.RoleAdminUser, ChannelSecretView))
				assert.True(t, Can(45, common.RoleAdminUser, ChannelRead))
				for _, userID := range []int{46, 47, 48} {
					assert.False(t, Can(userID, common.RoleAdminUser, ChannelRead))
				}
				assert.False(t, Can(51, common.RoleAdminUser, ChannelOperate), "reseed must not erase a scoped role restriction")
				assert.True(t, Can(50, common.RoleAdminUser, ChannelSensitiveWrite))
				assert.False(t, Can(99, common.RoleCommonUser, ChannelRead))
				var stored []model.CasbinRule
				require.NoError(t, db.Where("id IN ?", ids).Order("id").Find(&stored).Error)
				assert.Equal(t, rules, stored, "legacy rows must remain available for administrator review")
			}
		})
	}
}

func TestSetUserPermissionsStoresOnlyOverrides(t *testing.T) {
	db := newAuthzTestDB(t)
	require.NoError(t, Init(db))

	require.NoError(t, SetUserPermissions(42, PermissionsMap{
		ResourceChannel: {
			ActionRead:           true,
			ActionOperate:        true,
			ActionWrite:          false,
			ActionSensitiveWrite: true,
			ActionSecretView:     false,
			"unknown":            true,
		},
		"unknown": {
			ActionRead: true,
		},
	}))

	assert.True(t, Can(42, common.RoleAdminUser, ChannelSensitiveWrite))
	assert.False(t, Can(42, common.RoleAdminUser, ChannelWrite))
	assert.Equal(t, PermissionsMap{
		ResourceChannel: {
			ActionRead:           true,
			ActionOperate:        true,
			ActionWrite:          false,
			ActionSensitiveWrite: true,
			ActionSecretView:     false,
		},
		ResourceTaskPlugin: {
			ActionBind: false,
		},
		ResourceAudit: {ActionRead: false},
	}, ExplicitUserPermissions(42))
	assert.Equal(t, PermissionsMap{
		ResourceChannel: {
			ActionSensitiveWrite: true,
			ActionWrite:          false,
		},
	}, ExplicitUserOverrides(42))

	var userPolicyCount int64
	require.NoError(t, db.Model(&model.CasbinRule{}).Where("v0 = ?", UserSubject(42)).Count(&userPolicyCount).Error)
	assert.Equal(t, int64(2), userPolicyCount)

	require.NoError(t, SetUserPermissions(42, PermissionsMap{ResourceChannel: {
		ActionRead:           true,
		ActionOperate:        true,
		ActionWrite:          true,
		ActionSensitiveWrite: false,
		ActionSecretView:     false,
	}}))
	assert.False(t, Can(42, common.RoleAdminUser, ChannelSensitiveWrite))
	assert.Equal(t, PermissionsMap{
		ResourceChannel: {
			ActionRead:           true,
			ActionOperate:        true,
			ActionWrite:          true,
			ActionSensitiveWrite: false,
			ActionSecretView:     false,
		},
		ResourceTaskPlugin: {
			ActionBind: false,
		},
		ResourceAudit: {ActionRead: false},
	}, ExplicitUserPermissions(42))
	assert.Empty(t, ExplicitUserOverrides(42))
}

func TestClearUserAuthorizationRemovesOverrides(t *testing.T) {
	db := newAuthzTestDB(t)
	require.NoError(t, Init(db))

	require.NoError(t, SetUserPermissions(90, PermissionsMap{ResourceChannel: {
		ActionWrite:          false,
		ActionSensitiveWrite: true,
	}}))

	assert.True(t, Can(90, common.RoleAdminUser, ChannelSensitiveWrite))
	assert.False(t, Can(90, common.RoleAdminUser, ChannelWrite))

	require.NoError(t, ClearUserAuthorization(90))

	assert.Empty(t, ExplicitUserOverrides(90))
	assert.True(t, Can(90, common.RoleAdminUser, ChannelRead))
	assert.True(t, Can(90, common.RoleAdminUser, ChannelWrite))
	assert.False(t, Can(90, common.RoleAdminUser, ChannelSensitiveWrite))
	assert.False(t, Can(90, common.RoleCommonUser, ChannelRead))
}

func TestSetUserPermissionsInTxDoesNotMutateEnforcerBeforeReload(t *testing.T) {
	db := newAuthzTestDB(t)
	require.NoError(t, Init(db))

	require.NoError(t, db.Transaction(func(tx *gorm.DB) error {
		return SetUserPermissionsInTx(tx, 42, PermissionsMap{ResourceChannel: {
			ActionRead:           true,
			ActionOperate:        true,
			ActionWrite:          true,
			ActionSensitiveWrite: true,
			ActionSecretView:     false,
		}})
	}))

	assert.False(t, Can(42, common.RoleAdminUser, ChannelSensitiveWrite))
	require.NoError(t, ReloadPolicy())
	assert.True(t, Can(42, common.RoleAdminUser, ChannelSensitiveWrite))
}

func TestSetUserPermissionsInTxRollbackLeavesNoPolicy(t *testing.T) {
	db := newAuthzTestDB(t)
	require.NoError(t, Init(db))

	tx := db.Begin()
	require.NoError(t, tx.Error)
	require.NoError(t, SetUserPermissionsInTx(tx, 43, PermissionsMap{ResourceChannel: {
		ActionSensitiveWrite: true,
	}}))
	require.NoError(t, tx.Rollback().Error)
	require.NoError(t, ReloadPolicy())

	assert.False(t, Can(43, common.RoleAdminUser, ChannelSensitiveWrite))
	var count int64
	require.NoError(t, db.Model(&model.CasbinRule{}).Where("v0 = ?", UserSubject(43)).Count(&count).Error)
	assert.Equal(t, int64(0), count)
}

func TestAdapterAddPolicyIsIdempotent(t *testing.T) {
	db := newAuthzTestDB(t)
	adapter := newGormAdapter(db)
	rule := []string{UserSubject(55), ResourceChannel, ActionSensitiveWrite, EffectAllow}

	require.NoError(t, adapter.AddPolicy("p", "p", rule))
	require.NoError(t, adapter.AddPolicy("p", "p", rule))

	var count int64
	require.NoError(t, db.Model(&model.CasbinRule{}).Where(
		"ptype = ? AND v0 = ? AND v1 = ? AND v2 = ? AND v3 = ?",
		"p",
		UserSubject(55),
		ResourceChannel,
		ActionSensitiveWrite,
		EffectAllow,
	).Count(&count).Error)
	assert.Equal(t, int64(1), count)
}

func TestCapabilitiesUseCatalogShape(t *testing.T) {
	db := newAuthzTestDB(t)
	require.NoError(t, Init(db))

	capabilities := Capabilities(7, common.RoleAdminUser)

	assert.True(t, capabilities[ResourceChannel][ActionRead])
	assert.True(t, capabilities[ResourceChannel][ActionOperate])
	assert.True(t, capabilities[ResourceChannel][ActionWrite])
	assert.False(t, capabilities[ResourceChannel][ActionSensitiveWrite])
	assert.False(t, capabilities[ResourceChannel][ActionSecretView])
	assert.False(t, capabilities[ResourceTaskPlugin][ActionBind])
}

func TestTaskPluginBindIsRootOnlyUntilGranted(t *testing.T) {
	db := newAuthzTestDB(t)
	require.NoError(t, Init(db))

	var bindAction *ActionDefinition
	for _, resource := range Catalog() {
		if resource.Resource != ResourceTaskPlugin {
			continue
		}
		assert.Equal(t, "Task Plugin", resource.LabelKey)
		for i := range resource.Actions {
			if resource.Actions[i].Action == ActionBind {
				bindAction = &resource.Actions[i]
			}
		}
	}
	require.NotNil(t, bindAction)
	assert.Equal(t, "Bind task plugins", bindAction.LabelKey)
	assert.Equal(t, "List registered task plugins and bind them when creating or editing task plugin channels.", bindAction.DescriptionKey)
	assert.Empty(t, bindAction.DefaultRoles)

	assert.False(t, Can(2, common.RoleAdminUser, TaskPluginBind))
	assert.True(t, Can(1, common.RoleRootUser, TaskPluginBind))

	enforcer := currentEnforcer()
	require.NotNil(t, enforcer)
	_, err := enforcer.AddPolicy(RoleSubject(BuiltInRoleAdmin), ResourceTaskPlugin, ActionBind, EffectAllow)
	require.NoError(t, err)
	assert.True(t, Can(2, common.RoleAdminUser, TaskPluginBind))

	_, err = enforcer.RemovePolicy(RoleSubject(BuiltInRoleAdmin), ResourceTaskPlugin, ActionBind, EffectAllow)
	require.NoError(t, err)
	assert.False(t, Can(2, common.RoleAdminUser, TaskPluginBind))
}

// --- Stage 7.4.1 startup idempotency matrix (T1-T16) -----------------------

type seedSnapshot struct {
	Rules []model.CasbinRule
	Roles []model.AuthzRole
	Caps  map[int]PermissionsMap
}

func loadCasbinRules(t *testing.T, db *gorm.DB) []model.CasbinRule {
	t.Helper()
	var rules []model.CasbinRule
	require.NoError(t, db.Order("id asc").Find(&rules).Error)
	return rules
}

// forceMasterNode enables master-side seeding for tests that open their own
// connection (real MySQL/PostgreSQL) instead of going through newAuthzTestDB.
func forceMasterNode(t *testing.T) {
	t.Helper()
	wasMaster := common.IsMasterNode
	common.IsMasterNode = true
	t.Cleanup(func() {
		common.IsMasterNode = wasMaster
	})
}

func loadAuthzRoles(t *testing.T, db *gorm.DB) []model.AuthzRole {
	t.Helper()
	var roles []model.AuthzRole
	require.NoError(t, db.Order("id asc").Find(&roles).Error)
	return roles
}

func effectiveCapabilities() map[int]PermissionsMap {
	return map[int]PermissionsMap{
		common.RoleRootUser:   Capabilities(1, common.RoleRootUser),
		common.RoleAdminUser:  Capabilities(2, common.RoleAdminUser),
		common.RoleCommonUser: Capabilities(3, common.RoleCommonUser),
	}
}

func captureSeedSnapshot(t *testing.T, db *gorm.DB) seedSnapshot {
	t.Helper()
	return seedSnapshot{
		Rules: loadCasbinRules(t, db),
		Roles: loadAuthzRoles(t, db),
		Caps:  effectiveCapabilities(),
	}
}

func sqliteSequence(t *testing.T, db *gorm.DB, table string) int64 {
	t.Helper()
	var seq int64
	require.NoError(t, db.Raw(
		"SELECT COALESCE((SELECT seq FROM sqlite_sequence WHERE name = ?), 0)", table,
	).Scan(&seq).Error)
	return seq
}

// assertEffectiveBaseline pins the formal authorization decisions that must hold
// after any startup: root is implicitly all-powerful, admin gets exactly the
// channel read/operate/write baseline, and a common user gets nothing. It is the
// T9/T10 guard against silent permission expansion or contraction.
func assertEffectiveBaseline(t *testing.T, caps map[int]PermissionsMap) {
	t.Helper()

	root := caps[common.RoleRootUser]
	assert.True(t, root[ResourceChannel][ActionRead])
	assert.True(t, root[ResourceChannel][ActionSensitiveWrite])
	assert.True(t, root[ResourceChannel][ActionSecretView])
	assert.True(t, root[ResourceAudit][ActionRead])
	assert.True(t, root[ResourceTaskPlugin][ActionBind])

	admin := caps[common.RoleAdminUser]
	assert.True(t, admin[ResourceChannel][ActionRead])
	assert.True(t, admin[ResourceChannel][ActionOperate])
	assert.True(t, admin[ResourceChannel][ActionWrite])
	assert.False(t, admin[ResourceChannel][ActionSensitiveWrite], "admin must not gain sensitive_write")
	assert.False(t, admin[ResourceChannel][ActionSecretView], "admin must not gain secret_view")
	assert.False(t, admin[ResourceAudit][ActionRead])
	assert.False(t, admin[ResourceTaskPlugin][ActionBind])

	commonCaps := caps[common.RoleCommonUser]
	assert.False(t, commonCaps[ResourceChannel][ActionRead])
	assert.False(t, commonCaps[ResourceChannel][ActionWrite])
	assert.False(t, commonCaps[ResourceAudit][ActionRead])
	assert.False(t, commonCaps[ResourceTaskPlugin][ActionBind])
}

// runSeedIdempotencyContract is the dialect-agnostic proof that repeated
// migration + Init is physically idempotent: after the first seeding, further
// startups write nothing new — casbin_rule and authz_roles keep identical rows
// AND identical surrogate IDs, and effective decisions never drift. When
// sqliteSequences is true it also asserts the SQLite autoincrement counters stop
// advancing, which is the exact symptom that failed Stage 7.4 integrity.
func runSeedIdempotencyContract(t *testing.T, db *gorm.DB, sqliteSequences bool) {
	t.Helper()

	var first seedSnapshot
	for iteration := range 3 {
		require.NoError(t, db.AutoMigrate(&model.CasbinRule{}, &model.AuthzRole{}))
		require.NoError(t, Init(db))
		snap := captureSeedSnapshot(t, db)

		if iteration == 0 {
			require.Len(t, snap.Rules, len(PermissionsForRole(BuiltInRoleAdmin)))
			require.Len(t, snap.Roles, len(builtInRoles))
			assertEffectiveBaseline(t, snap.Caps)
			first = snap
			continue
		}
		assert.Equal(t, first.Rules, snap.Rules, "casbin rows+IDs must be identical on re-init %d", iteration+1)
		assert.Equal(t, first.Roles, snap.Roles, "authz_roles rows+IDs must be identical on re-init %d", iteration+1)
		assert.Equal(t, first.Caps, snap.Caps, "effective permissions must not drift on re-init %d", iteration+1)
	}

	if sqliteSequences {
		assert.Equal(t, int64(len(first.Rules)), sqliteSequence(t, db, "casbin_rule"),
			"casbin_rule autoincrement must not advance on repeat startup")
		assert.Equal(t, int64(len(first.Roles)), sqliteSequence(t, db, "authz_roles"),
			"authz_roles autoincrement must not advance on repeat startup")
	}
}

// TestSeedRepeatsArePhysicallyIdempotent covers T1-T5, T9, T10, T13, T14 on the
// staging dialect (SQLite).
func TestSeedRepeatsArePhysicallyIdempotent(t *testing.T) {
	db := newAuthzTestDB(t)
	runSeedIdempotencyContract(t, db, true)
}

// TestSeedReconcilesStaleAndMissingBuiltInPolicies covers T6: a legitimate drift
// (one desired rule removed, one stale unscoped built-in rule injected) is
// reconciled by a diff — the missing rule returns, the stale rule is dropped, and
// the untouched rules keep their original IDs.
func TestSeedReconcilesStaleAndMissingBuiltInPolicies(t *testing.T) {
	db := newAuthzTestDB(t)
	require.NoError(t, Init(db))

	findRule := func(rules []model.CasbinRule, resource, action string) *model.CasbinRule {
		for i := range rules {
			if rules[i].V0 == RoleSubject(BuiltInRoleAdmin) &&
				rules[i].V1 == resource && rules[i].V2 == action {
				return &rules[i]
			}
		}
		return nil
	}

	before := loadCasbinRules(t, db)
	readBefore := findRule(before, ResourceChannel, ActionRead)
	operateBefore := findRule(before, ResourceChannel, ActionOperate)
	writeBefore := findRule(before, ResourceChannel, ActionWrite)
	require.NotNil(t, readBefore)
	require.NotNil(t, operateBefore)
	require.NotNil(t, writeBefore)

	// Drift: drop a genuinely-desired rule and inject a stale unscoped built-in
	// grant that is no longer part of the formal defaults.
	require.NoError(t, db.Delete(&model.CasbinRule{}, writeBefore.Id).Error)
	stale := model.CasbinRule{
		Ptype: "p", V0: RoleSubject(BuiltInRoleAdmin),
		V1: ResourceAudit, V2: ActionRead, V3: EffectAllow,
	}
	require.NoError(t, db.Create(&stale).Error)

	require.NoError(t, Init(db))

	after := loadCasbinRules(t, db)
	require.Len(t, after, len(PermissionsForRole(BuiltInRoleAdmin)))

	readAfter := findRule(after, ResourceChannel, ActionRead)
	operateAfter := findRule(after, ResourceChannel, ActionOperate)
	writeAfter := findRule(after, ResourceChannel, ActionWrite)
	require.NotNil(t, readAfter)
	require.NotNil(t, operateAfter)
	require.NotNil(t, writeAfter, "missing desired rule must be re-added")

	assert.Equal(t, readBefore.Id, readAfter.Id, "unchanged rule must keep its ID")
	assert.Equal(t, operateBefore.Id, operateAfter.Id, "unchanged rule must keep its ID")
	assert.Nil(t, findRule(after, ResourceAudit, ActionRead), "stale built-in grant must be removed")

	assert.True(t, Can(2, common.RoleAdminUser, ChannelWrite))
	assert.False(t, Can(2, common.RoleAdminUser, AuditRead), "stale grant must not leave admin with audit read")
}

// TestSeedPreservesCustomRolesAndPolicies covers T7/T8: administrator-defined
// roles and policies are never touched by startup reconciliation.
func TestSeedPreservesCustomRolesAndPolicies(t *testing.T) {
	db := newAuthzTestDB(t)
	require.NoError(t, Init(db))

	customRole := model.AuthzRole{
		Key: "custom-manager", Name: "Custom Manager", Description: "ops",
		BuiltIn: false, Enabled: true, Sort: 50,
	}
	require.NoError(t, db.Create(&customRole).Error)
	customPolicy := model.CasbinRule{
		Ptype: "p", V0: RoleSubject("custom-manager"),
		V1: ResourceChannel, V2: ActionSensitiveWrite, V3: EffectAllow,
	}
	require.NoError(t, db.Create(&customPolicy).Error)

	for range 2 {
		require.NoError(t, Init(db))
	}

	var role model.AuthzRole
	require.NoError(t, db.First(&role, customRole.Id).Error)
	assert.Equal(t, "Custom Manager", role.Name)
	assert.False(t, role.BuiltIn)

	var policy model.CasbinRule
	require.NoError(t, db.First(&policy, customPolicy.Id).Error)
	assert.Equal(t, RoleSubject("custom-manager"), policy.V0)
	assert.Equal(t, ActionSensitiveWrite, policy.V2)
}

// TestSeedConcurrentStartupProducesNoDuplicates covers T11: several master
// startups racing on the same database must not create duplicate roles or
// policies nor surface unique-key errors.
func TestSeedConcurrentStartupProducesNoDuplicates(t *testing.T) {
	db := newAuthzTestDB(t)
	adapter := newGormAdapter(db)
	desired := builtInDefaultPolicies()

	const workers = 8
	var wg sync.WaitGroup
	errs := make([]error, workers)
	for i := range workers {
		wg.Go(func() {
			if err := db.Transaction(func(tx *gorm.DB) error {
				if err := seedBuiltInRoles(tx); err != nil {
					return err
				}
				return reconcileBuiltInRolePolicies(tx)
			}); err != nil {
				errs[i] = err
				return
			}
			for _, rule := range desired {
				if err := adapter.AddPolicy("p", "p", []string{rule.V0, rule.V1, rule.V2, rule.V3}); err != nil {
					errs[i] = err
					return
				}
			}
		})
	}
	wg.Wait()
	for i := range workers {
		require.NoError(t, errs[i], "concurrent startup %d must not error", i)
	}

	var roleCount, policyCount int64
	require.NoError(t, db.Model(&model.AuthzRole{}).Count(&roleCount).Error)
	require.NoError(t, db.Model(&model.CasbinRule{}).Count(&policyCount).Error)
	assert.Equal(t, int64(len(builtInRoles)), roleCount, "no duplicate roles")
	assert.Equal(t, int64(len(desired)), policyCount, "no duplicate policies")
}

// TestSeedDoesNotTouchOtherTables covers T12: startup writes are confined to
// authz_roles and casbin_rule; unrelated business/ledger tables and their
// autoincrement sequences are untouched.
func TestSeedDoesNotTouchOtherTables(t *testing.T) {
	db := newAuthzTestDB(t)
	sqlDB, err := db.DB()
	require.NoError(t, err)

	tables := []string{"billing_records", "ledger_entries"}
	for _, table := range tables {
		_, err := sqlDB.Exec("CREATE TABLE " + table + " (id INTEGER PRIMARY KEY AUTOINCREMENT, amount INTEGER NOT NULL)")
		require.NoError(t, err)
		_, err = sqlDB.Exec("INSERT INTO " + table + " (amount) VALUES (100), (200)")
		require.NoError(t, err)
	}

	snapshot := func() map[string][]int64 {
		result := make(map[string][]int64, len(tables))
		for _, table := range tables {
			rows, err := sqlDB.Query("SELECT id, amount FROM " + table + " ORDER BY id")
			require.NoError(t, err)
			var flat []int64
			for rows.Next() {
				var id, amount int64
				require.NoError(t, rows.Scan(&id, &amount))
				flat = append(flat, id, amount)
			}
			require.NoError(t, rows.Close())
			flat = append(flat, sqliteSequence(t, db, table))
			result[table] = flat
		}
		return result
	}

	before := snapshot()
	for range 2 {
		require.NoError(t, Init(db))
	}
	assert.Equal(t, before, snapshot(), "startup must not touch unrelated tables")
}

// TestSeedIdempotencyMySQL covers T15. It is skipped unless a scratch MySQL DSN
// is supplied; it never silently reports PASS without a real server.
func TestSeedIdempotencyMySQL(t *testing.T) {
	dsn := os.Getenv("AUTHZ_TEST_MYSQL_DSN")
	if dsn == "" {
		t.Skip("AUTHZ_TEST_MYSQL_DSN not set; skipping real MySQL verification")
	}
	db, err := gorm.Open(mysql.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	forceMasterNode(t)
	resetDialectTables(t, db)
	runSeedIdempotencyContract(t, db, false)
}

// TestSeedIdempotencyPostgres covers T16. It is skipped unless a scratch
// PostgreSQL DSN is supplied; it never silently reports PASS without a real
// server.
func TestSeedIdempotencyPostgres(t *testing.T) {
	dsn := os.Getenv("AUTHZ_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("AUTHZ_TEST_POSTGRES_DSN not set; skipping real PostgreSQL verification")
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	forceMasterNode(t)
	resetDialectTables(t, db)
	runSeedIdempotencyContract(t, db, false)
}

// resetDialectTables drops the authz tables before and after a real-server run so
// the contract always starts from an empty schema and leaves no residue.
func resetDialectTables(t *testing.T, db *gorm.DB) {
	t.Helper()
	drop := func() {
		require.NoError(t, db.Exec("DROP TABLE IF EXISTS casbin_rule").Error)
		require.NoError(t, db.Exec("DROP TABLE IF EXISTS authz_roles").Error)
	}
	drop()
	t.Cleanup(drop)
}
