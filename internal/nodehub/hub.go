package nodehub

import (
	"encoding/json"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

const (
	protocolVersion = 1
	pingInterval    = 25 * time.Second
	requestTimeout  = 15 * time.Second
)

type Hub struct {
	mu         sync.RWMutex
	conns      map[int64]*connState
	pending    map[int64]chan *Response
	nextReqID  int64
	upgrader   websocket.Upgrader
	resolve    func(token string) (nodeID int64, uid string, ok bool)
	onStateChange func(nodeID int64, online bool)
}

type connState struct {
	mu        sync.Mutex
	nodeID    int64
	uid       string
	conn      *websocket.Conn
	send      chan []byte
	closed    bool
	lastPong  time.Time
}

type helloMsg struct {
	Hostname string `json:"hostname"`
	UID      string `json:"uid"`
	Version  string `json:"version"`
	Role     string `json:"role"`
}

// exported aliases used by the nodeagent package to marshal/unmarshal
// the same wire format without duplicating the struct tags.
type HelloMsg = helloMsg

type requestMsg struct {
	Type   string          `json:"type"`
	ID     int64           `json:"id,omitempty"`
	Method string          `json:"method,omitempty"`
	Path   string          `json:"path,omitempty"`
	Body   json.RawMessage `json:"body,omitempty"`
}

// RequestMsg is the exported alias of requestMsg (see HelloMsg).
type RequestMsg = requestMsg

type responseMsg struct {
	Type   string          `json:"type"`
	ID     int64           `json:"id"`
	Status int             `json:"status"`
	Body   json.RawMessage `json:"body,omitempty"`
	Error  string          `json:"error,omitempty"`
}

// ResponseMsg is the exported alias of responseMsg (see HelloMsg).
type ResponseMsg = responseMsg

type pingMsg struct {
	Type string `json:"type"`
}

type statusMsg struct {
	Type    string          `json:"type"`
	Summary json.RawMessage `json:"summary,omitempty"`
}

type Response struct {
	Status int
	Body   []byte
	Error  string
}

func NewHub(resolve func(token string) (int64, string, bool), onStateChange func(int64, bool)) *Hub {
	return &Hub{
		conns:       make(map[int64]*connState),
		pending:     make(map[int64]chan *Response),
		upgrader:    websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }},
		resolve:     resolve,
		onStateChange: onStateChange,
	}
}

func (h *Hub) ServeWS(w http.ResponseWriter, r *http.Request) {
	auth := r.Header.Get("Authorization")
	if auth == "" || len(auth) < 7 || auth[:7] != "Bearer " {
		http.Error(w, "missing bearer token", http.StatusUnauthorized)
		return
	}
	token := auth[7:]

	nodeID, expectedUID, ok := h.resolve(token)
	if !ok {
		http.Error(w, "invalid token", http.StatusUnauthorized)
		return
	}

	ws, err := h.upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}

	cs := &connState{
		nodeID:   nodeID,
		uid:      expectedUID,
		conn:     ws,
		send:     make(chan []byte, 16),
		lastPong: time.Now(),
	}

	h.register(cs)
	go cs.writeLoop()
	cs.readLoop(h)
	h.unregister(cs)
}

func (h *Hub) register(cs *connState) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if old, exists := h.conns[cs.nodeID]; exists {
		old.close()
	}
	h.conns[cs.nodeID] = cs
	if h.onStateChange != nil {
		h.onStateChange(cs.nodeID, true)
	}
}

func (h *Hub) unregister(cs *connState) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.conns[cs.nodeID] == cs {
		delete(h.conns, cs.nodeID)
		if h.onStateChange != nil {
			h.onStateChange(cs.nodeID, false)
		}
	}
	close(cs.send)
}

func (h *Hub) Request(nodeID int64, method, path string, body []byte, timeout time.Duration) (*Response, error) {
	h.mu.RLock()
	cs, ok := h.conns[nodeID]
	h.mu.RUnlock()
	if !ok {
		return nil, ErrNodeOffline
	}

	h.mu.Lock()
	h.nextReqID++
	reqID := h.nextReqID
	respCh := make(chan *Response, 1)
	h.pending[reqID] = respCh
	h.mu.Unlock()

	msg := requestMsg{
		Type:   "req",
		ID:     reqID,
		Method: method,
		Path:   path,
		Body:   body,
	}
	data, _ := json.Marshal(msg)
	select {
	case cs.send <- data:
	case <-time.After(2 * time.Second):
		h.mu.Lock()
		delete(h.pending, reqID)
		h.mu.Unlock()
		return nil, ErrSendTimeout
	}

	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case resp := <-respCh:
		return resp, nil
	case <-timer.C:
		h.mu.Lock()
		delete(h.pending, reqID)
		h.mu.Unlock()
		return nil, ErrRequestTimeout
	}
}

func (h *Hub) IsOnline(nodeID int64) bool {
	h.mu.RLock()
	defer h.mu.RUnlock()
	_, ok := h.conns[nodeID]
	return ok
}

func (h *Hub) ConnectedNodes() []int64 {
	h.mu.RLock()
	defer h.mu.RUnlock()
	ids := make([]int64, 0, len(h.conns))
	for id := range h.conns {
		ids = append(ids, id)
	}
	return ids
}

func (cs *connState) readLoop(h *Hub) {
	defer cs.close()
	cs.conn.SetReadLimit(4 << 20)
	cs.conn.SetPongHandler(func(string) error {
		cs.lastPong = time.Now()
		return nil
	})
	for {
		_, data, err := cs.conn.ReadMessage()
		if err != nil {
			return
		}
		var msg requestMsg
		if err := json.Unmarshal(data, &msg); err != nil {
			continue
		}
		switch msg.Type {
		case "resp":
			var resp responseMsg
			if err := json.Unmarshal(data, &resp); err == nil {
				h.mu.RLock()
				ch, ok := h.pending[resp.ID]
				h.mu.RUnlock()
				if ok {
					select {
					case ch <- &Response{Status: resp.Status, Body: resp.Body, Error: resp.Error}:
					default:
					}
					h.mu.Lock()
					delete(h.pending, resp.ID)
					h.mu.Unlock()
				}
			}
		case "ping":
			_ = cs.writeJSON(pingMsg{Type: "pong"})
		case "hello":
			var hello helloMsg
			if err := json.Unmarshal(data, &hello); err == nil {
				h.mu.Lock()
				cs.uid = hello.UID
				h.mu.Unlock()
			}
		case "status":
		}
	}
}

func (cs *connState) writeLoop() {
	ticker := time.NewTicker(pingInterval)
	defer ticker.Stop()
	for {
		select {
		case data, ok := <-cs.send:
			if !ok {
				return
			}
			if err := cs.conn.WriteMessage(websocket.TextMessage, data); err != nil {
				return
			}
		case <-ticker.C:
			if time.Since(cs.lastPong) > 2*pingInterval {
				return
			}
			_ = cs.writeJSON(pingMsg{Type: "ping"})
		}
	}
}

func (cs *connState) writeJSON(v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return cs.conn.WriteMessage(websocket.TextMessage, data)
}

func (cs *connState) close() {
	cs.mu.Lock()
	if cs.closed {
		cs.mu.Unlock()
		return
	}
	cs.closed = true
	cs.mu.Unlock()
	cs.conn.Close()
}

var ErrNodeOffline = &hubError{"node offline"}
var ErrSendTimeout = &hubError{"send timeout"}
var ErrRequestTimeout = &hubError{"request timeout"}

type hubError struct{ msg string }

func (e *hubError) Error() string { return e.msg }