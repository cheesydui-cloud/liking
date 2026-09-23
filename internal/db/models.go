package db

type User struct {
	ID                 int64    `json:"id"`
	Username           string   `json:"username"`
	PasswordHash       string   `json:"-"`
	PasswordPlain      string   `json:"-"`
	Role               string   `json:"role"`
	Remark             string   `json:"remark"`
	Enabled            bool     `json:"enabled"`
	ExpiresAt          int64    `json:"expires_at"`
	TrafficLimit       *int64   `json:"traffic_limit"`
	UsedUp             int64    `json:"used_up"`
	UsedDown           int64    `json:"used_down"`
	CycleStart         int64    `json:"cycle_start"`
	SubToken           string   `json:"sub_token"`
	CreatedAt          int64    `json:"created_at"`
	PackageID          *int64   `json:"package_id,omitempty"`
	PackageName        string   `json:"package_name,omitempty"`
	PkgExpires         int64    `json:"package_expires_at,omitempty"`
	TrafficCap         int64    `json:"traffic_cap,omitempty"`
	BilledBytes        int64    `json:"billed_bytes"`
	Direction          string   `json:"direction,omitempty"`
	TOTPEnabled        bool     `json:"totp_enabled"`
	TOTPSecret         string   `json:"-"`
	TrafficResetDay    int      `json:"traffic_reset_day"`
	QuotaRatio         int      `json:"quota_ratio,omitempty"`
	SpeedLimit         int64    `json:"speed_limit"` // KB/s; 0 = unlimited; 1 Mbps = 125
	NetUpBps           int64    `json:"net_up_bps"`
	NetDownBps         int64    `json:"net_down_bps"`
	SubRulePreset      string   `json:"sub_rule_preset"`
	SubRuleCategories  []string `json:"sub_rule_categories"`
	SiteDenyCategories []string `json:"site_deny_categories"`
	SiteDenyDomains    []string `json:"site_deny_domains"`
	SiteFilterMode     string   `json:"site_filter_mode"`
}

type Server struct {
	ID              int64  `json:"id"`
	Name            string `json:"name"`
	PublicHost      string `json:"public_host"`
	Token           string `json:"token,omitempty"`
	Online          int    `json:"online"`
	LastSeen        int64  `json:"last_seen"`
	AgentVer        string `json:"agent_ver"`
	OS              string `json:"os"`
	Arch            string `json:"arch"`
	ConnectIP       string `json:"connect_ip"`
	ConfigRev       string `json:"config_rev"`
	LastError       string `json:"last_error"`
	LastErrorAt     int64  `json:"last_error_at"`
	Cores           string `json:"cores"`
	CreatedAt       int64  `json:"created_at"`
	TrafficLimit    int64  `json:"traffic_limit"`
	UsedUp          int64  `json:"used_up"`
	UsedDown        int64  `json:"used_down"`
	NetUpBps        int64  `json:"net_up_bps"`
	NetDownBps      int64  `json:"net_down_bps"`
	DiskFree        int64  `json:"disk_free,omitempty"`
	DiskTotal       int64  `json:"disk_total,omitempty"`
	MemAvail        int64  `json:"mem_avail,omitempty"`
	MemTotal        int64  `json:"mem_total,omitempty"`
	LoadMilli       int64  `json:"load_milli,omitempty"`
	Conns           int    `json:"conns,omitempty"`
	CoresRunning    string `json:"cores_running,omitempty"`
	OverQuota       bool   `json:"over_quota,omitempty"`
	NeedsUpgrade    bool   `json:"needs_upgrade,omitempty"`
	NeedsReinstall  bool   `json:"needs_reinstall,omitempty"`
	PortMin         int    `json:"port_min"`
	PortMax         int    `json:"port_max"`
	ExpiresAt       int64  `json:"expires_at"`
	TrafficResetDay int    `json:"traffic_reset_day"`
	DisableIPv6     bool   `json:"disable_ipv6"`
	CanPushCores    bool   `json:"can_push_cores,omitempty"`
	CanPushNft      bool   `json:"can_push_nft,omitempty"`
}

type Certificate struct {
	ID        int64  `json:"id"`
	Name      string `json:"name"`
	CertPEM   string `json:"cert_pem,omitempty"`
	KeyPEM    string `json:"key_pem,omitempty"`
	Domains   string `json:"domains"`
	Source    string `json:"source"`
	ExpiresAt int64  `json:"expires_at"`
	AcmeEmail string `json:"acme_email,omitempty"`
	LastError string `json:"last_error,omitempty"`
	AutoRenew bool   `json:"auto_renew"`
}

type Package struct {
	ID                 int64     `json:"id"`
	Name               string    `json:"name"`
	TrafficBytes       int64     `json:"traffic_bytes"`
	CycleDays          int       `json:"cycle_days"`
	ResetDay           int       `json:"reset_day"`
	Direction          string    `json:"direction"`
	CreatedAt          int64     `json:"created_at"`
	InboundIDs         []int64   `json:"inbound_ids,omitempty"`
	Multipliers        []float64 `json:"multipliers,omitempty"`
	ServerIDs          []int64   `json:"server_ids"`
	SpeedLimit         int64     `json:"speed_limit"` // KB/s; 0 = unlimited; 1 Mbps = 125
	SubRulePreset      string    `json:"sub_rule_preset"`
	SubRuleCategories  []string  `json:"sub_rule_categories"`
	SiteDenyCategories []string  `json:"site_deny_categories"`
	SiteDenyDomains    []string  `json:"site_deny_domains"`
	SiteFilterMode     string    `json:"site_filter_mode"`
}

type Inbound struct {
	ID            int64  `json:"id"`
	ServerID      int64  `json:"server_id"`
	Name          string `json:"name"`
	Profile       string `json:"profile"`
	Protocol      string `json:"protocol"`
	Network       string `json:"network"`
	Security      string `json:"security"`
	Core          string `json:"core"`
	Listen        string `json:"listen"`
	Port          int    `json:"port"`
	Enabled       bool   `json:"enabled"`
	Settings      string `json:"settings"`
	CertID        *int64 `json:"cert_id"`
	LineKind      string `json:"line_kind"`
	ExitInboundID *int64 `json:"exit_inbound_id"`
	ExitURI       string `json:"exit_uri"`
	RejectCN      bool   `json:"reject_cn"`
	CreatedAt     int64  `json:"created_at"`
	ServerName    string `json:"server_name,omitempty"`
	ServerHost    string `json:"server_host,omitempty"`
	ConnectIP     string `json:"connect_ip,omitempty"`
	ServerOnline  int    `json:"server_online,omitempty"`
	UsedUp        int64  `json:"used_up,omitempty"`
	UsedDown      int64  `json:"used_down,omitempty"`
}

type Client struct {
	ID        int64  `json:"id"`
	InboundID int64  `json:"inbound_id"`
	UserID    int64  `json:"user_id"`
	Email     string `json:"email"`
	UUID      string `json:"uuid"`
	Password  string `json:"password"`
	Username  string `json:"username"`
	Enabled   bool   `json:"enabled"`
}

type Audit struct {
	ID     int64  `json:"id"`
	At     int64  `json:"at"`
	UserID *int64 `json:"user_id,omitempty"`
	Action string `json:"action"`
	Detail string `json:"detail"`
}
