package ops

import "portguard/internal/store"

// MappingTemplate is a ready-made mapping preset (like haproxy-manager's templates),
// rendered by the frontend into the "new mapping" form.
type MappingTemplate struct {
	ID          string        `json:"id"`
	Name        string        `json:"name"`
	Description string        `json:"description"`
	Mapping     store.Mapping `json:"mapping"`
}

// Templates mirrors haproxy-manager's template list adapted to PortGuard's model.
func Templates() []MappingTemplate {
	return []MappingTemplate{
		{
			ID: "tcp-lb", Name: "TCP Load Balancer",
			Description: "Layer-4 load balancing of raw TCP traffic (HAProxy).",
			Mapping: store.Mapping{
				Engine: "haproxy", Protocol: "tcp", ListenIP: "0.0.0.0", ListenPort: 3306,
				Balance: "leastconn",
				Targets: []store.Target{{Host: "127.0.0.1", Port: 3307}, {Host: "127.0.0.1", Port: 3308}},
			},
		},
		{
			ID: "http-lb", Name: "HTTP Load Balancer",
			Description: "Layer-7 HTTP load balancing across several backends.",
			Mapping: store.Mapping{
				Engine: "nginx", Protocol: "http", ListenIP: "0.0.0.0", ListenPort: 8080,
				ServerNames: []string{"app.example.com"}, Balance: "least_conn",
				Targets: []store.Target{{Host: "127.0.0.1", Port: 3000}, {Host: "127.0.0.1", Port: 3001}},
			},
		},
		{
			ID: "https-proxy", Name: "HTTPS Reverse Proxy",
			Description: "TLS termination with a certificate from the SSL Certs page.",
			Mapping: store.Mapping{
				Engine: "nginx", Protocol: "https", ListenIP: "0.0.0.0", ListenPort: 443,
				ServerNames: []string{"app.example.com"}, HTTP2: true, SSLCertID: nil,
				Targets: []store.Target{{Host: "127.0.0.1", Port: 3000}},
			},
		},
		{
			ID: "tls-passthrough", Name: "TLS Passthrough",
			Description: "Raw TCP forwarding without terminating TLS (HAProxy).",
			Mapping: store.Mapping{
				Engine: "haproxy", Protocol: "tcp", ListenIP: "0.0.0.0", ListenPort: 8443,
				Targets: []store.Target{{Host: "10.0.0.10", Port: 8443}},
			},
		},
		{
			ID: "websocket", Name: "WebSocket App",
			Description: "HTTP proxy with upgrade headers and long tunnel timeouts.",
			Mapping: store.Mapping{
				Engine: "nginx", Protocol: "http", ListenIP: "0.0.0.0", ListenPort: 8090,
				ServerNames: []string{"ws.example.com"}, WebSocket: true,
				Targets: []store.Target{{Host: "127.0.0.1", Port: 9000}},
			},
		},
		{
			ID: "redirect", Name: "Redirect (HTTP → HTTPS)",
			Description: "301 redirect of all requests to another URL, no backend.",
			Mapping: store.Mapping{
				Engine: "nginx", Protocol: "http", ListenIP: "0.0.0.0", ListenPort: 80,
				ServerNames: []string{"example.com"}, RedirectTo: "https://example.com",
			},
		},
		{
			ID: "port-forward", Name: "Port Forward (UDP)",
			Description: "Forward a UDP port to a remote endpoint (nginx stream).",
			Mapping: store.Mapping{
				Engine: "nginx", Protocol: "udp", ListenIP: "0.0.0.0", ListenPort: 5353,
				Targets: []store.Target{{Host: "1.1.1.1", Port: 53}},
			},
		},
		{
			ID: "xray-path", Name: "Xray Path Routing (ws/httpupgrade/xhttp)",
			Description: "One TLS listener; /<prefix>/<port> routes to a local Xray inbound's port (ws, httpupgrade, xhttp).",
			Mapping: store.Mapping{
				Engine: "nginx", Protocol: "https", ListenIP: "0.0.0.0", ListenPort: 443,
				ServerNames: []string{"node.example.com"}, HTTP2: false, SSLCertID: nil,
				PathRoutes: []store.PathRoute{
					{Transport: store.PathTransportWS, Prefix: "ws", MinPort: 10000, MaxPort: 10003},
					{Transport: store.PathTransportHU, Prefix: "hu", MinPort: 10000, MaxPort: 10003},
					{Transport: store.PathTransportXHTTP, Prefix: "xhttp", MinPort: 10000, MaxPort: 10003},
				},
			},
		},
	}
}
