package accountpool

// Platform / access-type catalog. This is the single source of truth for what
// the pool module can manage today, which executor owns each access type, and
// what capability claims exist. Claims separate "status" (whether the
// capability is admitted for external use) from "evidence" (how well the
// claim is substantiated). Nothing is admitted in phase two: admission per
// platform + access type + model + adapter version is a later phase, so every
// claim below stays status=unverified even when source-level evidence exists.

import "github.com/QuantumNous/new-api/common"

const (
	ClaimStatusUnverified = "unverified"
	ClaimStatusVerified   = "verified"
	ClaimStatusRefuted    = "refuted"
)

const (
	EvidenceDocumentation = "documentation"
	EvidenceSource        = "source"
	EvidenceTest          = "test"
	EvidenceLive          = "live"
	EvidenceNone          = "none"
)

// Adapter version bumps whenever the underlying executor integration changes
// enough to invalidate earlier capability verification.
const AdapterVersion = 1

const (
	ExecutorCliproxyAPI = "cliproxyapi"
	ExecutorGrok2API    = "grok2api"
	ExecutorNone        = "none"
)

// Operation identifiers of the adapter contract. SupportedOps/UnsupportedOps
// are explicit per access type; unsupported operations must return a clear
// error, never a fake success.
const (
	OpListAccounts     = "list_accounts"
	OpStartAuth        = "start_auth"
	OpAuthStatus       = "auth_status"
	OpCompleteCallback = "complete_callback"
	OpCancelAuth       = "cancel_auth"
	OpPause            = "pause"
	OpResume           = "resume"
	OpReauth           = "reauth"
	OpDelete           = "delete"
	OpCapabilities     = "capabilities"
	OpQuota            = "quota"
	OpHealth           = "health"
	OpSyncRunStatus    = "sync_run_status"
	OpCookieImport     = "cookie_import"
	OpBrowserBridge    = "browser_bridge"
)

const (
	AuthMethodOAuthBrowser  = "oauth_browser"
	AuthMethodOAuthDevice   = "oauth_device"
	AuthMethodCallbackURL   = "callback_url_paste"
	AuthMethodCookieImport  = "cookie_import"
	AuthMethodBrowserBridge = "browser_bridge"
)

type CapabilityClaim struct {
	// Key is a stable capability identifier, e.g. "text_chat", "tools",
	// "image_input", "image_generation", "long_context", "exact_usage".
	Key      string `json:"key"`
	Status   string `json:"status"`
	Evidence string `json:"evidence"`
	Note     string `json:"note,omitempty"`
}

type AccessTypeSpec struct {
	Type           string            `json:"type"`
	Label          string            `json:"label"`
	ExecutorId     string            `json:"executor_id"`
	AuthMethods    []string          `json:"auth_methods"`
	SupportedOps   []string          `json:"supported_ops"`
	UnsupportedOps []string          `json:"unsupported_ops"`
	Disabled       bool              `json:"disabled"`
	DisabledReason string            `json:"disabled_reason,omitempty"`
	Claims         []CapabilityClaim `json:"claims"`
}

type PlatformSpec struct {
	Platform    string           `json:"platform"`
	Label       string           `json:"label"`
	AccessTypes []AccessTypeSpec `json:"access_types"`
}

type ExecutorDescriptor struct {
	Id                 string `json:"id"`
	Type               string `json:"type"`
	Connected          bool   `json:"connected"`
	Verified           bool   `json:"verified"`
	IsolationAttested  bool   `json:"isolation_attested"`
	EndpointConfigured bool   `json:"endpoint_configured"`
	// RequiredActions lists the environment actions that must be completed
	// (and attested) before authorization is unlocked for this executor.
	RequiredActions []string `json:"required_actions,omitempty"`
	Note            string   `json:"note,omitempty"`
}

func baseManagementOps() []string {
	return []string{OpListAccounts, OpStartAuth, OpAuthStatus, OpCompleteCallback, OpCancelAuth,
		OpPause, OpResume, OpReauth, OpDelete, OpCapabilities, OpHealth, OpSyncRunStatus}
}

func executorClaims(usageEvidence string) []CapabilityClaim {
	return []CapabilityClaim{
		{Key: "text_chat", Status: ClaimStatusUnverified, Evidence: usageEvidence, Note: "model-specific source and directed validation required"},
		{Key: "stream_text", Status: ClaimStatusUnverified, Evidence: usageEvidence},
		{Key: "tools", Status: ClaimStatusUnverified, Evidence: EvidenceNone},
		{Key: "image_input", Status: ClaimStatusUnverified, Evidence: EvidenceNone},
		{Key: "image_generation", Status: ClaimStatusUnverified, Evidence: EvidenceNone},
		{Key: "long_context", Status: ClaimStatusUnverified, Evidence: EvidenceNone},
		{Key: "exact_usage", Status: ClaimStatusUnverified, Evidence: usageEvidence, Note: "presence, provenance and end-to-end directed validation required; real-account testing is not mandatory"},
	}
}

func Catalog() []PlatformSpec {
	return []PlatformSpec{
		{
			Platform: "claude",
			Label:    "Claude (Claude Code OAuth)",
			AccessTypes: []AccessTypeSpec{{
				Type: "oauth_browser", Label: "OAuth 浏览器授权（支持粘贴回调地址）",
				ExecutorId:   ExecutorCliproxyAPI,
				AuthMethods:  []string{AuthMethodOAuthBrowser, AuthMethodCallbackURL},
				SupportedOps: baseManagementOps(), UnsupportedOps: []string{OpQuota, OpCookieImport, OpBrowserBridge},
				Claims: executorClaims(EvidenceSource),
			}},
		},
		{
			Platform: "codex",
			Label:    "Codex (ChatGPT 订阅 OAuth)",
			AccessTypes: []AccessTypeSpec{
				{
					Type: "oauth_browser", Label: "OAuth 浏览器授权",
					ExecutorId:   ExecutorCliproxyAPI,
					AuthMethods:  []string{AuthMethodOAuthBrowser, AuthMethodCallbackURL},
					SupportedOps: baseManagementOps(), UnsupportedOps: []string{OpQuota, OpCookieImport, OpBrowserBridge},
					Claims: executorClaims(EvidenceSource),
				},
				{
					Type: "oauth_device", Label: "OAuth 设备码授权",
					ExecutorId:   ExecutorCliproxyAPI,
					AuthMethods:  []string{AuthMethodOAuthDevice},
					SupportedOps: baseManagementOps(), UnsupportedOps: []string{OpQuota, OpCookieImport, OpBrowserBridge},
					Claims: executorClaims(EvidenceSource),
				},
			},
		},
		{
			Platform: "kimi_coding",
			Label:    "Kimi Coding（设备码 OAuth，非网页聊天）",
			AccessTypes: []AccessTypeSpec{{
				Type: "oauth_device", Label: "OAuth 设备码授权",
				ExecutorId:   ExecutorCliproxyAPI,
				AuthMethods:  []string{AuthMethodOAuthDevice},
				SupportedOps: baseManagementOps(), UnsupportedOps: []string{OpQuota, OpCookieImport, OpBrowserBridge},
				Claims: executorClaims(EvidenceSource),
			}},
		},
		{
			Platform: "gemini_code_assist",
			Label:    "Gemini CLI / Code Assist OAuth",
			AccessTypes: []AccessTypeSpec{{
				Type: "oauth_browser", Label: "OAuth 浏览器授权（授权码粘贴）",
				ExecutorId:   ExecutorNone,
				AuthMethods:  []string{AuthMethodOAuthBrowser, AuthMethodCallbackURL},
				SupportedOps: []string{}, UnsupportedOps: baseManagementOps(),
				Disabled:       true,
				DisabledReason: "executor_gap: CLIProxyAPI v8.0.4 不支持 Code Assist OAuth 凭据（filestore 跳过 type=gemini，源码确认）；需专用执行器，未经单独授权不引入，不用官方 API Key 替代",
				Claims:         []CapabilityClaim{{Key: "all", Status: ClaimStatusUnverified, Evidence: EvidenceNone}},
			}},
		},
		{
			Platform: "grok_build",
			Label:    "Grok Build（OAuth/设备码）",
			AccessTypes: []AccessTypeSpec{{
				Type: "oauth_device", Label: "OAuth 设备码授权",
				ExecutorId:   ExecutorGrok2API,
				AuthMethods:  []string{AuthMethodOAuthDevice},
				SupportedOps: baseManagementOps(), UnsupportedOps: []string{OpQuota, OpCookieImport, OpBrowserBridge},
				Claims: executorClaims(EvidenceNone),
			}},
		},
		{
			Platform: "grok_web_sso",
			Label:    "Grok Web（SSO Cookie）",
			AccessTypes: []AccessTypeSpec{{
				Type: "cookie_import", Label: "SSO 会话导入（站长手动粘贴）",
				ExecutorId:   ExecutorGrok2API,
				AuthMethods:  []string{AuthMethodCookieImport},
				SupportedOps: baseManagementOps(), UnsupportedOps: []string{OpQuota, OpBrowserBridge},
				Claims: executorClaims(EvidenceNone),
			}},
		},
		{
			Platform: "grok_console",
			Label:    "Grok Console（SSO）",
			AccessTypes: []AccessTypeSpec{{
				Type: "cookie_import", Label: "SSO 会话导入（站长手动粘贴）",
				ExecutorId:   ExecutorGrok2API,
				AuthMethods:  []string{AuthMethodCookieImport},
				SupportedOps: baseManagementOps(), UnsupportedOps: []string{OpQuota, OpBrowserBridge},
				Claims: executorClaims(EvidenceNone),
			}},
		},
		{
			Platform: "qwen_web",
			Label:    "Qwen 网页桥接",
			AccessTypes: []AccessTypeSpec{{
				Type: "browser_bridge", Label: "浏览器桥接（单连接）",
				ExecutorId:   ExecutorNone,
				AuthMethods:  []string{AuthMethodBrowserBridge},
				SupportedOps: []string{}, UnsupportedOps: baseManagementOps(),
				Disabled:       true,
				DisabledReason: "admission: 单连接桥接、伪流式（64 字符切片）、仅保留 12 轮/32000 字符、无图片输入、本地字符估算 token（源码确认）；不满足精确计量准入，保留研究项",
				Claims:         []CapabilityClaim{{Key: "all", Status: ClaimStatusUnverified, Evidence: EvidenceNone}},
			}},
		},
		{
			Platform: "deepseek_web",
			Label:    "DeepSeek 网页账号",
			AccessTypes: []AccessTypeSpec{{
				Type: "cookie_import", Label: "网页账号会话导入",
				ExecutorId:   ExecutorNone,
				AuthMethods:  []string{AuthMethodCookieImport},
				SupportedOps: []string{}, UnsupportedOps: baseManagementOps(),
				Disabled:       true,
				DisabledReason: "admission: 参考实现 ds2api 已归档且使用本地估算 token（源码确认）；不满足精确计量准入，保持禁用",
				Claims:         []CapabilityClaim{{Key: "all", Status: ClaimStatusUnverified, Evidence: EvidenceNone}},
			}},
		},
	}
}

func Lookup(platform, accessType string) (*PlatformSpec, *AccessTypeSpec, bool) {
	catalog := Catalog()
	for pi := range catalog {
		if catalog[pi].Platform != platform {
			continue
		}
		for ti := range catalog[pi].AccessTypes {
			if catalog[pi].AccessTypes[ti].Type == accessType {
				return &catalog[pi], &catalog[pi].AccessTypes[ti], true
			}
		}
		return nil, nil, false
	}
	return nil, nil, false
}

func Executors() []ExecutorDescriptor {
	dedicated := common.PoolCliproxyDedicated()
	cliproxyNote := "v8.0.4 (commit d33f63f8e3d98428440ebca5a5b6a981a61ff71e)。管理适配器已实现但从未连接运行（标记：尚未运行验证）。" +
		"隔离前提（源码确认 v8.0.4 无创建时隔离：新凭据文件 disabled:false 且被热加载即刻纳入调度，无隔离/审批机制）：需要独立执行器实例" +
		"（独立凭据目录与管理密钥，不与任何渠道共用实例）、配置 access.api-keys（未配置即开放访问），" +
		"并以 CLEARAPI_POOL_CLIPROXY_DEDICATED=true 确认；未确认前授权接口拒绝执行。" +
		"注意：DEDICATED 仅是配置声明，不是持续隔离保证——授权完成到禁用生效之间在本实例内存在调度窗口。" +
		"第三阶段开通对外渠道前必须满足其一：(a) 执行器侧提供创建即隔离/审批机制（v8.0.4 无此能力，需上游改造或补丁）；" +
		"(b) 分离授权暂存与服务执行器，并在执行器每次账号选择/发送前核对账号及模型准入。仅入口鉴权不能代替账号级隔离。未满足前服务端拒绝池渠道开通和调用。"
	var cliproxyActions []string
	if !dedicated {
		cliproxyActions = []string{
			"部署独立执行器实例（独立凭据目录/管理密钥，不经任何渠道引用）",
			"配置 access.api-keys（未配置即开放访问）",
			"设置 CLEARAPI_POOL_CLIPROXY_DEDICATED=true 确认隔离",
		}
	}
	return []ExecutorDescriptor{
		{
			Id: ExecutorCliproxyAPI, Type: "cliproxyapi",
			Connected: false, Verified: false, EndpointConfigured: CliproxyEndpointConfigured(),
			IsolationAttested: dedicated, RequiredActions: cliproxyActions, Note: cliproxyNote,
		},
		{
			Id: ExecutorGrok2API, Type: "grok2api",
			Connected: false, Verified: false, EndpointConfigured: false,
			IsolationAttested: false,
			RequiredActions:   []string{"固定 v3.1.6 (commit 906b9493b099d192381c698d4e320fafeccb851c) 镜像并部署独立实例；实现管理适配器"},
			Note:              "v3.1.6 (commit 906b9493b099d192381c698d4e320fafeccb851c)。仅完成研究，管理适配器未实现、未部署；引入属于环境动作。",
		},
	}
}
