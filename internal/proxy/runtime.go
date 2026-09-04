package proxy

import (
	"encoding/csv"
	"errors"
	"fmt"
	"net"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// RuntimeClient talks to the HAProxy Runtime API over its stats socket
// ("show info", "show stat", "set server ... state ..."), mirroring the
// haproxy-manager runtime module.
type RuntimeClient struct {
	SocketPath string
	Dial       DialFunc // injectable for tests; nil = unix socket dial
	Timeout    time.Duration
}

// DialFunc opens a connection to the stats socket.
type DialFunc func(path string, timeout time.Duration) (net.Conn, error)

func defaultDial(path string, timeout time.Duration) (net.Conn, error) {
	return net.DialTimeout("unix", path, timeout)
}

func (c *RuntimeClient) dialer() DialFunc {
	if c.Dial != nil {
		return c.Dial
	}
	return defaultDial
}

var nameRe = regexp.MustCompile(`^[A-Za-z0-9_.\-]+$`)

// Send writes one command and reads the full response (socket closes on EOF).
func (c *RuntimeClient) Send(cmd string) (string, error) {
	if c.SocketPath == "" {
		return "", errors.New("runtime socket path is not configured")
	}
	timeout := c.Timeout
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	conn, err := c.dialer()(c.SocketPath, timeout)
	if err != nil {
		return "", fmt.Errorf("cannot connect to runtime socket %s: %w", c.SocketPath, err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(timeout))
	if _, err := conn.Write([]byte(strings.TrimRight(cmd, "\n") + "\n")); err != nil {
		return "", err
	}
	if uw, ok := conn.(interface{ CloseWrite() error }); ok {
		_ = uw.CloseWrite() // signal EOF so HAProxy flushes and closes
	}
	var sb strings.Builder
	buf := make([]byte, 4096)
	for {
		n, err := conn.Read(buf)
		if n > 0 {
			sb.Write(buf[:n])
		}
		if err != nil {
			break // EOF or timeout ends the response
		}
	}
	return sb.String(), nil
}

// ShowInfo returns key/value pairs from "show info".
func (c *RuntimeClient) ShowInfo() (map[string]string, error) {
	out, err := c.Send("show info")
	if err != nil {
		return nil, err
	}
	info := map[string]string{}
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		k, v, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		info[strings.TrimSpace(k)] = strings.TrimSpace(v)
	}
	return info, nil
}

// StatRow is one CSV row of "show stat".
type StatRow map[string]string

// ShowStat returns parsed rows from "show stat".
func (c *RuntimeClient) ShowStat() ([]StatRow, error) {
	out, err := c.Send("show stat")
	if err != nil {
		return nil, err
	}
	return ParseStatCSV(out), nil
}

// ParseStatCSV parses "show stat" CSV output; the first line is the "# pxname,..." header.
func ParseStatCSV(text string) []StatRow {
	var header []string
	var rows []StatRow
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimRight(line, "\r")
		if strings.TrimSpace(line) == "" {
			continue
		}
		if strings.HasPrefix(line, "#") {
			dec := csv.NewReader(strings.NewReader(strings.TrimSpace(strings.TrimPrefix(line, "#"))))
			if rec, err := dec.Read(); err == nil {
				header = rec
			}
			continue
		}
		if header == nil {
			continue
		}
		dec := csv.NewReader(strings.NewReader(line))
		rec, err := dec.Read()
		if err != nil {
			continue
		}
		row := StatRow{}
		for i, v := range rec {
			if i < len(header) {
				row[header[i]] = v
			}
		}
		rows = append(rows, row)
	}
	return rows
}

// Summary aggregates stat rows for the dashboard (like haproxy-manager's summarize_stats).
func SummaryStats(rows []StatRow) map[string]any {
	s := map[string]any{
		"frontends": 0, "backends": 0, "servers": 0,
		"up": 0, "down": 0, "maint": 0,
		"sessions": 0, "bytes_in": 0, "bytes_out": 0,
	}
	totalSess, bin, bout := 0, 0, 0
	fe, be, sv, up, down, maint := 0, 0, 0, 0, 0, 0
	for _, r := range rows {
		svname := r["svname"]
		switch {
		case svname == "FRONTEND":
			fe++
			totalSess += atoiDefault(r["scur"])
			bin += atoiDefault(r["bin"])
			bout += atoiDefault(r["bout"])
		case svname == "BACKEND":
			be++
		case svname == "STATS" || r["pxname"] == "stats":
			// internal stats socket entry — not a real server
		default:
			sv++
			status := r["status"]
			switch {
			case strings.HasPrefix(status, "UP"), status == "OPEN", status == "no check":
				up++
			case strings.Contains(status, "DOWN"):
				down++
			case strings.Contains(status, "MAINT"):
				maint++
			default:
				up++
			}
		}
	}
	s["frontends"], s["backends"], s["servers"] = fe, be, sv
	s["up"], s["down"], s["maint"] = up, down, maint
	s["sessions"], s["bytes_in"], s["bytes_out"] = totalSess, bin, bout
	return s
}

func atoiDefault(s string) int {
	n, _ := strconv.Atoi(strings.TrimSpace(s))
	return n
}

// SetServerState sets a backend server state (ready | drain | maint).
func (c *RuntimeClient) SetServerState(backend, server, state string) error {
	if state != "ready" && state != "drain" && state != "maint" {
		return errors.New("state must be ready, drain or maint")
	}
	if !nameRe.MatchString(backend) || !nameRe.MatchString(server) {
		return errors.New("invalid backend or server name")
	}
	out, err := c.Send(fmt.Sprintf("set server %s/%s state %s", backend, server, state))
	if err != nil {
		return err
	}
	if strings.Contains(strings.ToLower(out), "error") || strings.Contains(strings.ToLower(out), "unknown") {
		return errors.New(strings.TrimSpace(out))
	}
	return nil
}

// EnableServer enables traffic to a backend server ("enable server").
func (c *RuntimeClient) EnableServer(backend, server string) error {
	if !nameRe.MatchString(backend) || !nameRe.MatchString(server) {
		return errors.New("invalid backend or server name")
	}
	_, err := c.Send(fmt.Sprintf("enable server %s/%s", backend, server))
	return err
}

// DisableServer removes a backend server from the rotation ("disable server").
func (c *RuntimeClient) DisableServer(backend, server string) error {
	if !nameRe.MatchString(backend) || !nameRe.MatchString(server) {
		return errors.New("invalid backend or server name")
	}
	_, err := c.Send(fmt.Sprintf("disable server %s/%s", backend, server))
	return err
}
