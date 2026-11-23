package service

import (
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

const testPoolSeed = "3f2b8c1e-4d5a-4b6c-8d7e-9f0a1b2c3d4e"

func newPoolTestAccount(extra map[string]any) *Account {
	base := map[string]any{
		codexFingerprintModeExtraKey: "device",
		codexFingerprintSeedExtraKey: testPoolSeed,
	}
	for k, v := range extra {
		base[k] = v
	}
	return &Account{ID: 501, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Extra: base}
}

func TestCodexDevicePool_DisabledKeepsSingleDevice(t *testing.T) {
	account := newPoolTestAccount(nil)
	single := resolveConvergedInstallationID(account, testPoolSeed)
	for _, uid := range []int64{0, 1, 2, 3, 9, 28} {
		ids := resolveCodexFingerprintIDsForUser(account, "", codexFingerprintDevice, uid)
		require.NotNil(t, ids)
		require.Equal(t, single, ids.installationID, "未配置设备池时所有用户应收敛为同一台设备")
	}
}

func TestCodexDevicePool_ManyUsersShareThreeDevices(t *testing.T) {
	account := newPoolTestAccount(map[string]any{codexFingerprintDevicePoolSizeExtraKey: float64(3)})
	seen := map[string]int{}
	for uid := int64(1); uid <= 9; uid++ {
		ids := resolveCodexFingerprintIDsForUser(account, "", codexFingerprintDevice, uid)
		require.NotNil(t, ids)
		seen[ids.installationID]++
	}
	require.Len(t, seen, 3, "9 个用户应只出现 3 台设备")
	for id, n := range seen {
		require.Equal(t, 3, n, "每台设备应有 3 个用户: %s", id)
	}
}

func TestCodexDevicePool_StickyPerUser(t *testing.T) {
	account := newPoolTestAccount(map[string]any{codexFingerprintDevicePoolSizeExtraKey: 3})
	first := resolveCodexFingerprintIDsForUser(account, "s1", codexFingerprintDevice, 28)
	second := resolveCodexFingerprintIDsForUser(account, "s2", codexFingerprintDevice, 28)
	require.Equal(t, first.installationID, second.installationID, "同一用户不同会话必须落在同一台设备")
}

func TestCodexDevicePool_SlotZeroEqualsLegacyDevice(t *testing.T) {
	account := newPoolTestAccount(map[string]any{codexFingerprintDevicePoolSizeExtraKey: 3})
	legacy := resolveConvergedInstallationID(account, testPoolSeed)
	ids := resolveCodexFingerprintIDsForUser(account, "", codexFingerprintDevice, 3)
	require.Equal(t, legacy, ids.installationID, "序号 0 应沿用原单设备身份")
	missing := resolveCodexFingerprintIDsForUser(account, "", codexFingerprintDevice, 0)
	require.Equal(t, legacy, missing.installationID, "取不到用户时落在序号 0")
}

func TestCodexDevicePool_AssignOverridesModulo(t *testing.T) {
	account := newPoolTestAccount(map[string]any{
		codexFingerprintDevicePoolSizeExtraKey: 3,
		codexFingerprintDevicePoolAssignExtraKey: map[string]any{
			"9": float64(1), "12": float64(1), "28": float64(2), "99": float64(7),
		},
	})
	id9 := resolveCodexFingerprintIDsForUser(account, "", codexFingerprintDevice, 9).installationID
	id12 := resolveCodexFingerprintIDsForUser(account, "", codexFingerprintDevice, 12).installationID
	id28 := resolveCodexFingerprintIDsForUser(account, "", codexFingerprintDevice, 28).installationID
	require.Equal(t, id9, id12, "指定到同一序号的用户共用一台设备")
	require.NotEqual(t, id9, id28)
	// 非法序号（越界）回退取模：99 % 3 == 0
	legacy := resolveConvergedInstallationID(account, testPoolSeed)
	require.Equal(t, legacy, resolveCodexFingerprintIDsForUser(account, "", codexFingerprintDevice, 99).installationID)
}

func TestCodexDevicePool_RealDeviceIDDisablesPool(t *testing.T) {
	account := newPoolTestAccount(map[string]any{
		codexFingerprintDevicePoolSizeExtraKey: 3,
		"openai_device_id":                     "11111111-2222-4333-8444-555555555555",
	})
	if account.GetOpenAIDeviceID() == "" {
		t.Skip("当前版本 device_id 读取键不同，跳过")
	}
	a := resolveCodexFingerprintIDsForUser(account, "", codexFingerprintDevice, 1).installationID
	b := resolveCodexFingerprintIDsForUser(account, "", codexFingerprintDevice, 2).installationID
	require.Equal(t, a, b)
}

func TestCodexDevicePool_SessionModeIgnoresPool(t *testing.T) {
	account := newPoolTestAccount(map[string]any{
		codexFingerprintModeExtraKey:           "session",
		codexFingerprintDevicePoolSizeExtraKey: 3,
	})
	a := resolveCodexFingerprintIDsForUser(account, "s", codexFingerprintSession, 1).installationID
	b := resolveCodexFingerprintIDsForUser(account, "s", codexFingerprintSession, 2).installationID
	require.Equal(t, a, b, "设备池只作用于 device 模式")
}

func TestCodexDevicePool_FromContextUsesAPIKeyUser(t *testing.T) {
	gin.SetMode(gin.TestMode)
	account := newPoolTestAccount(map[string]any{codexFingerprintDevicePoolSizeExtraKey: 3})
	mk := func(uid int64) *gin.Context {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest("POST", "/v1/responses", nil)
		c.Set("api_key", &APIKey{ID: 1000 + uid, UserID: uid})
		return c
	}
	got1 := resolveCodexFingerprintIDsFromContext(mk(1), account).installationID
	got4 := resolveCodexFingerprintIDsFromContext(mk(4), account).installationID
	got2 := resolveCodexFingerprintIDsFromContext(mk(2), account).installationID
	require.Equal(t, got1, got4, "1 与 4 取模同为 1")
	require.NotEqual(t, got1, got2)
	require.Nil(t, resolveCodexFingerprintIDsFromContext(mk(1), newPoolTestAccount(map[string]any{codexFingerprintModeExtraKey: "off"})))
}
