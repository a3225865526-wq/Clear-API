package model

import (
	"github.com/QuantumNous/new-api/common"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClosedLaunchOverridesUnsafeOptions(t *testing.T) {
	t.Setenv("CLEARAPI_CLOSED_LAUNCH", "true")
	oldOptions := common.OptionMap
	oldRegister, oldPassword := common.RegisterEnabled, common.PasswordRegisterEnabled
	oldQuota, oldRetry := common.QuotaForNewUser, common.RetryTimes
	common.OptionMap = make(map[string]string)
	t.Cleanup(func() {
		common.OptionMap = oldOptions
		common.RegisterEnabled, common.PasswordRegisterEnabled = oldRegister, oldPassword
		common.QuotaForNewUser, common.RetryTimes = oldQuota, oldRetry
	})
	for key, value := range map[string]string{"RegisterEnabled": "true", "PasswordRegisterEnabled": "true", "QuotaForNewUser": "500000", "RetryTimes": "3"} {
		require.NoError(t, updateOptionMap(key, value))
	}
	assert.False(t, common.RegisterEnabled)
	assert.False(t, common.PasswordRegisterEnabled)
	assert.Zero(t, common.QuotaForNewUser)
	assert.Zero(t, common.RetryTimes)
}

func TestValidateOptionValueRejectsInvalidMaxTokenAutoGroups(t *testing.T) {
	for _, value := range []string{"", "0", "-1", "1.5", "invalid"} {
		t.Run(value, func(t *testing.T) {
			assert.Error(t, validateOptionValue("MaxTokenAutoGroups", value))
		})
	}
	require.NoError(t, validateOptionValue("MaxTokenAutoGroups", "999999"))
}
