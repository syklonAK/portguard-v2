package nodeagent

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"portguard/internal/nodehub"
)

const (
	reconnectBase    = 1 * time.Second
	reconnectMax     = 30 * time.Second
	helloTimeout     = 5 * time.Second
	statusInterval   = 30 * time.Second
	writeTimeout     = 5 * time.Second
)

type Config struct {
	MasterURL string
	Token     string
	Router    http.Handler
	Version   string
	Role      string
	UID       string
	Hostname  string
}

type Agent struct {
	cfg       Config
	dialer    *websocket.Dialer
	mu        sync.Mutex
	conn      *websocket.Conn
	sendCh    chan []byte
	stopCh    chan struct{}
	wg        sync.WaitGroup
	connected bool
}

func Run(ctx context.Context, cfg Config) error {
	if cfg.MasterURL == "" {
		return nil
	}
	if cfg.Router == nil {
		panic("nodeagent: Router is required")
	}
	if cfg.Version == "" {
		cfg.Version = "unknown"
	}
	if cfg.Hostname == "" {
		cfg.Hostname, _ = getHostname()
	}
	if cfg.UID == "" {
		cfg.UID = "unknown"
	}

	a := &Agent{
		cfg:    cfg,
		dialer: websocket.DefaultDialer,
		sendCh: make(chan []byte, 16),
		stopCh: make(chan struct{}),
	}
	a.dialer.HandshakeTimeout = 10 * time.Second

	go a.runLoop(ctx)
	<-ctx.Done()
	a.stop()
	return nil
}

func (a *Agent) runLoop(ctx context.Context) {
	backoff := reconnectBase
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		err := a.connect(ctx)
		if err != nil {
			select {
			case <-ctx.Done():
				return
			case <-time.After(backoff):
				backoff *= 2
				if backoff > reconnectMax {
					backoff = reconnectMax
				}
			}
			continue
		}
		backoff = reconnectBase
	}
}

func (a *Agent) connect(ctx context.Context) error {
	u, err := url.Parse(a.cfg.MasterURL)
	if err != nil {
		return err
	}
	u.Path = strings.TrimRight(u.Path, "/") + "/api/node/ws"
	u.Scheme = map[string]string{"http": "ws", "https": "wss"}[u.Scheme]
	if u.Scheme == "" {
		u.Scheme = "ws"
	}

	header := http.Header{}
	header.Set("Authorization", "Bearer "+a.cfg.Token)

	conn, _, err := a.dialer.DialContext(ctx, u.String(), header)
	if err != nil {
		return err
	}

	a.mu.Lock()
	a.conn = conn
	a.connected = true
	a.mu.Unlock()

	a.wg.Add(1)
	go a.writeLoop()

	if err := a.sendHello(); err != nil {
		conn.Close()
		a.setDisconnected()
		return err
	}

	a.readLoop(ctx)
	a.setDisconnected()
	return nil
}

func (a *Agent) sendHello() error {
	hello := nodehub.HelloMsg{
		Hostname: a.cfg.Hostname,
		UID:      a.cfg.UID,
		Version:  a.cfg.Version,
		Role:     a.cfg.Role,
	}
	data, _ := json.Marshal(map[string]any{
		"type": "hello",
		"hostname": hello.Hostname,
		"uid": hello.UID,
		"version": hello.Version,
		"role": hello.Role,
	})
	return a.writeMessage(websocket.TextMessage, data)
}

func (a *Agent) readLoop(ctx context.Context) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	a.conn.SetReadLimit(4 << 20)
	a.conn.SetPongHandler(func(string) error { return nil })

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		_, data, err := a.conn.ReadMessage()
		if err != nil {
			return
		}

		var msg map[string]any
		if err := json.Unmarshal(data, &msg); err != nil {
			continue
		}

		msgType, _ := msg["type"].(string)
		switch msgType {
		case "req":
			go a.handleRequest(data)
		case "pong":
		case "ping":
			_ = a.writeJSON(map[string]string{"type": "pong"})
		}
	}
}

func (a *Agent) handleRequest(raw []byte) {
	var req nodehub.RequestMsg
	if err := json.Unmarshal(raw, &req); err != nil {
		a.sendResponse(req.ID, 400, nil, "invalid request")
		return
	}

	path := req.Path
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}

	bodyReader := strings.NewReader(string(req.Body))
	r := httptest.NewRequest(req.Method, path, bodyReader)
	if req.Body != nil {
		r.Header.Set("Content-Type", "application/json")
	}
	r.Header.Set("Authorization", "Bearer "+a.cfg.Token)

	w := httptest.NewRecorder()
	a.cfg.Router.ServeHTTP(w, r)

	a.sendResponse(req.ID, w.Code, w.Body.Bytes(), "")
}

func (a *Agent) sendResponse(reqID int64, status int, body []byte, errStr string) {
	resp := nodehub.ResponseMsg{
		Type:   "resp",
		ID:     reqID,
		Status: status,
		Body:   body,
		Error:  errStr,
	}
	data, _ := json.Marshal(resp)
	_ = a.writeMessage(websocket.TextMessage, data)
}

func (a *Agent) writeLoop() {
	defer a.wg.Done()
	ticker := time.NewTicker(statusInterval)
	defer ticker.Stop()

	for {
		select {
		case <-a.stopCh:
			return
		case data, ok := <-a.sendCh:
			if !ok {
				return
			}
			a.mu.Lock()
			if a.conn != nil {
				a.conn.SetWriteDeadline(time.Now().Add(writeTimeout))
				_ = a.conn.WriteMessage(websocket.TextMessage, data)
			}
			a.mu.Unlock()
		case <-ticker.C:
			if a.IsConnected() {
				_ = a.writeJSON(map[string]string{"type": "ping"})
			}
		}
	}
}

func (a *Agent) writeMessage(mt int, data []byte) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.conn == nil {
		return nil
	}
	a.conn.SetWriteDeadline(time.Now().Add(writeTimeout))
	return a.conn.WriteMessage(mt, data)
}

func (a *Agent) writeJSON(v any) error {
	data, _ := json.Marshal(v)
	return a.writeMessage(websocket.TextMessage, data)
}

func (a *Agent) IsConnected() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.connected
}

func (a *Agent) setDisconnected() {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.connected = false
	if a.conn != nil {
		a.conn.Close()
		a.conn = nil
	}
}

func (a *Agent) stop() {
	close(a.stopCh)
	a.setDisconnected()
	a.wg.Wait()
}

func getHostname() (string, error) {
	// Simple hostname detection
	return "portguard-agent", nil
}