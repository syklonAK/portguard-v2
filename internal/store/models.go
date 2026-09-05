package store

import (
	"database/sql"
	"encoding/json"
	"time"
)

type Admin struct {
	ID           int64      `json:"id"`
	Username     string     `json:"username"`
	PasswordHash string     `json:"-"`
	CreatedAt    time.Time  `json:"created_at"`
	LastLoginAt  *time.Time `json:"last_login_at"`
}

type Cert struct {
	ID        int64      `json:"id"`
	Name      string     `json:"name"`
	Type      string     `json:"type"` // manual | selfsigned
	CertPEM   string     `json:"-"`
	KeyPEM    string     `json:"-"`
	Domains   []string   `json:"domains"`
	ExpiresAt *time.Time `json:"expires_at"`
	CreatedAt time.Time  `json:"created_at"`
}

type Target struct {
	Host   string `json:"host"`
	Port   int    `json:"port"`
	Weight int    `json:"weight,omitempty"`
	Backup bool   `json:"backup,omitempty"`
}

// PathTransport is the dynamic-forwarding flavour routed by a path prefix.
type PathTransport string

const (
	PathTransportWS      PathTransport = "ws"        // WebSocket (requires Upgrade header)
	PathTransportHU      PathTransport = "httpupgrade" // HTTPUpgrade (requires Upgrade header)
	PathTransportXHTTP   PathTransport = "xhttp"     // XHTTP / split HTTP (no Upgrade, streaming)
)

// ValidPathTransport reports whether t is one of the supported transports.
func ValidPathTransport(t string) bool {
	return t == string(PathTransportWS) || t == string(PathTransportHU) || t == string(PathTransportXHTTP)
}

// PathRoute routes /<prefix>/<port> to 127.0.0.1:<port> for one transport
// (port-in-path dynamic forwarding, like Xray inbound behind one domain).
type PathRoute struct {
	Transport PathTransport `json:"transport"` // ws | httpupgrade | xhttp
	Prefix    string       `json:"prefix"`    // e.g. "ws" -> /ws/<port>
	MinPort   int          `json:"min_port"`  // allowed local port range
	MaxPort   int          `json:"max_port"`
}

// ConnEntry is one live TCP connection observed on the host (snapshot).
type ConnEntry struct {
	SrcIP      string `json:"src_ip"`
	SrcPort    int    `json:"src_port"`
	DstIP      string `json:"dst_ip"`
	DstPort    int    `json:"dst_port"`
	Process    string `json:"process"`
	PID        int    `json:"pid"`
	UID        int    `json:"uid"`
	State      string `json:"state"` // ESTAB mostly; listener entries show LISTEN
	Managed    bool   `json:"managed"`
	Inner      bool   `json:"inner"` // portguard panel / loopback-only services
	Self       bool   `json:"self"`  // connection to the panel itself
	FirstSeen  int64  `json:"first_seen"`
	LastSeen   int64  `json:"last_seen"`
}

// TopTalker aggregates live connections per source IP.
type TopTalker struct {
	SrcIP     string `json:"src_ip"`
	Conns     int    `json:"conns"`
	Targets   string `json:"targets"` // "ip:port, ip:port" (top 3)
	FirstSeen int64  `json:"first_seen"`
}

// ACLRule is one client-IP access rule (allow/deny), evaluated in order.
// If any "allow" rule exists, clients matching none of them are rejected.
type ACLRule struct {
	Action string `json:"action"` // allow | deny
	Value  string `json:"value"`  // IP or CIDR, e.g. 10.0.0.0/8
}

type Mapping struct {
	ID           int64             `json:"id"`
	Name         string            `json:"name"`
	Enabled      bool              `json:"enabled"`
	Engine       string            `json:"engine"`   // nginx | haproxy
	Protocol     string            `json:"protocol"` // http | https | tcp | udp
	ListenIP     string            `json:"listen_ip"`
	ListenPort   int               `json:"listen_port"`
	ServerNames  []string          `json:"server_names"`
	SSLCertID    *int64            `json:"ssl_cert_id"`
	RedirectTo   string            `json:"redirect_to"`
	WebSocket    bool              `json:"websocket"`
	HTTP2        bool              `json:"http2"`
	Targets      []Target          `json:"targets"`
	Balance      string            `json:"balance"` // LB algorithm; "" = engine default
	PathPrefix   string            `json:"path_prefix"`
	AccessRules  []ACLRule         `json:"access_rules"`
	ExtraHeaders map[string]string `json:"extra_headers"`
	PathRoutes   []PathRoute       `json:"path_routes"`
	HostHeader   string            `json:"host_header"` // upstream Host header (proxy_set_header Host)
	Decoy        string            `json:"decoy"`       // "" | builtin | custom — serve a real site on unmatched paths
	DecoyHTML    string            `json:"decoy_html"`  // custom HTML when decoy == "custom"
	Notes        string            `json:"notes"`
	CreatedAt    time.Time         `json:"created_at"`
	UpdatedAt    time.Time         `json:"updated_at"`
}

// ServerNode is a managed remote server profile: another PortGuard panel
// installation this master can configure and monitor over its node API.
type ServerNode struct {
	ID          int64      `json:"id"`
	Name        string     `json:"name"`
	Host        string     `json:"host"`       // public address of the node panel
	Port        int        `json:"port"`       // node panel port
	APIToken    string     `json:"-"`          // shared secret for the node API (never exposed)
	Role        string     `json:"role"`       // standalone | master | iran | foreign | generic
	Enabled     bool       `json:"enabled"`
	Notes       string     `json:"notes"`
	Status      string     `json:"status"`      // online | offline | unknown (last probe)
	LastSeen    *time.Time `json:"last_seen"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
}

// TunnelRelay is one Iran-side relay entry (Hedioum Pool Tunnel topology):
// users hit this server and traffic is relayed to the foreign node through
// the Hedioum SOCKS5 hub. mode: raw (passthrough) | tls (terminate here).
type TunnelRelay struct {
	ID          int64     `json:"id"`
	Name        string    `json:"name"`
	Mode        string    `json:"mode"` // raw | tls
	Enabled     bool      `json:"enabled"`
	TargetHost  string    `json:"target_host"`  // foreign node public address
	TargetPort  int       `json:"target_port"`  // foreign node port
	ListenIP    string    `json:"listen_ip"`    // raw mode: public listen IP
	ListenPort  int       `json:"listen_port"`   // raw mode: public listen port
	BridgePort  int       `json:"bridge_port"`   // tls mode: local dokodemo port
	UDP         bool      `json:"udp"`
	HostHeader  string    `json:"host_header"`  // tls mode: Host sent to the foreign node
	Domain      string    `json:"domain"`       // tls mode: public server_name
	SSLCertID   *int64    `json:"ssl_cert_id"`  // tls mode: cert for the local vhost
	Notes       string    `json:"notes"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type PortEntry struct {
	Port           int    `json:"port"`
	Proto          string `json:"proto"` // tcp | udp
	ListenIP       string `json:"listen_ip"`
	Process        string `json:"process"`
	PID            int    `json:"pid"`
	User           string `json:"user"`
	Classification string `json:"classification"`
	Managed        bool   `json:"managed"`
	Self           bool   `json:"self"` // the PortGuard panel itself
}

type TargetHealth struct {
	MappingID   int64      `json:"mapping_id"`
	TargetIndex int        `json:"target_index"`
	Host        string     `json:"host"`
	Port        int        `json:"port"`
	Status      string     `json:"status"` // up | down | unknown
	LatencyMS   float64    `json:"latency_ms"`
	FailCount   int        `json:"fail_count"`
	LastCheckAt *time.Time `json:"last_check_at"`
}

type AuditLog struct {
	ID        int64     `json:"id"`
	Actor     string    `json:"actor"`
	Action    string    `json:"action"`
	Detail    string    `json:"detail"`
	Status    string    `json:"status"` // ok | error
	CreatedAt time.Time `json:"created_at"`
}

func mustJSON(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

func scanMapping(row interface{ Scan(...any) error }) (Mapping, error) {
	var m Mapping
	var enabled, websocket, http2 int
	var serverNames, targets, headers, accessRules, balance, pathPrefix, pathRoutes sql.NullString
	var hostHeader, decoy, decoyHTML sql.NullString
	var sslCertID sql.NullInt64
	var createdTS, updatedTS int64
	err := row.Scan(&m.ID, &m.Name, &enabled, &m.Engine, &m.Protocol, &m.ListenIP, &m.ListenPort,
		&serverNames, &sslCertID, &m.RedirectTo, &websocket, &http2, &targets, &balance, &pathPrefix,
		&accessRules, &headers, &pathRoutes, &hostHeader, &decoy, &decoyHTML, &m.Notes,
		&createdTS, &updatedTS)
	if err != nil {
		return m, err
	}
	m.Enabled = enabled == 1
	m.WebSocket = websocket == 1
	m.HTTP2 = http2 == 1
	m.Balance = balance.String
	m.PathPrefix = pathPrefix.String
	if sslCertID.Valid {
		m.SSLCertID = &sslCertID.Int64
	}
	if serverNames.Valid && serverNames.String != "" {
		_ = json.Unmarshal([]byte(serverNames.String), &m.ServerNames)
	}
	if targets.Valid && targets.String != "" {
		_ = json.Unmarshal([]byte(targets.String), &m.Targets)
	}
	if accessRules.Valid && accessRules.String != "" {
		_ = json.Unmarshal([]byte(accessRules.String), &m.AccessRules)
	}
	if headers.Valid && headers.String != "" {
		_ = json.Unmarshal([]byte(headers.String), &m.ExtraHeaders)
	}
	if pathRoutes.Valid && pathRoutes.String != "" {
		_ = json.Unmarshal([]byte(pathRoutes.String), &m.PathRoutes)
	}
	m.HostHeader = hostHeader.String
	m.Decoy = decoy.String
	m.DecoyHTML = decoyHTML.String
	m.CreatedAt = time.Unix(createdTS, 0)
	m.UpdatedAt = time.Unix(updatedTS, 0)
	return m, nil
}

const mappingCols = `id, name, enabled, engine, protocol, listen_ip, listen_port, server_names,
ssl_cert_id, redirect_to, websocket, http2, targets, balance, path_prefix, access_rules,
extra_headers, path_routes, host_header, decoy, decoy_html, notes, created_at, updated_at`

func scanTunnelRelay(row interface{ Scan(...any) error }) (TunnelRelay, error) {
	var r TunnelRelay
	var enabled, udp int
	var sslCertID sql.NullInt64
	var createdTS, updatedTS int64
	err := row.Scan(&r.ID, &r.Name, &r.Mode, &enabled, &r.TargetHost, &r.TargetPort,
		&r.ListenIP, &r.ListenPort, &r.BridgePort, &udp, &r.HostHeader, &r.Domain,
		&sslCertID, &r.Notes, &createdTS, &updatedTS)
	if err != nil {
		return r, err
	}
	r.Enabled = enabled == 1
	r.UDP = udp == 1
	if sslCertID.Valid {
		r.SSLCertID = &sslCertID.Int64
	}
	r.CreatedAt = time.Unix(createdTS, 0)
	r.UpdatedAt = time.Unix(updatedTS, 0)
	return r, nil
}

const tunnelRelayCols = `id, name, mode, enabled, target_host, target_port, listen_ip,
listen_port, bridge_port, udp, host_header, domain, ssl_cert_id, notes, created_at, updated_at`
