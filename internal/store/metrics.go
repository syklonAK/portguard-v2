package store

import (
	"time"
)

// Code in this file was split out of store.go verbatim (v2.11 audit):
// same package, same API — only the file boundaries changed.

func (s *Store) InsertMetric(p MetricPoint) error {
	_, err := s.DB.Exec(`INSERT INTO node_metrics(node_id, cpu_percent, mem_percent, disk_percent, rx_bytes, tx_bytes, rx_bps, tx_bps, conns, ts)
		VALUES(?,?,?,?,?,?,?,?,?,?)`,
		p.NodeID, p.CPUPercent, p.MemPercent, p.DiskPercent, p.RxBytes, p.TxBytes, p.RxBps, p.TxBps, p.Conns, p.TS)
	return err
}
// QueryMetrics returns sampled points for a node in [from, to], downsampled
func (s *Store) QueryMetrics(nodeID int64, from, to int64) ([]MetricPoint, error) {
	rows, err := s.DB.Query(`SELECT node_id, cpu_percent, mem_percent, disk_percent, rx_bytes, tx_bytes, rx_bps, tx_bps, conns, ts
		FROM node_metrics WHERE node_id=? AND ts BETWEEN ? AND ? ORDER BY ts`, nodeID, from, to)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []MetricPoint
	for rows.Next() {
		var p MetricPoint
		if err := rows.Scan(&p.NodeID, &p.CPUPercent, &p.MemPercent, &p.DiskPercent, &p.RxBytes, &p.TxBytes, &p.RxBps, &p.TxBps, &p.Conns, &p.TS); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	// downsample: keep every Nth point so charts stay light
	if len(out) > 200 {
		step := len(out) / 200
		slim := make([]MetricPoint, 0, 201)
		for i := 0; i < len(out); i += step {
			slim = append(slim, out[i])
		}
		slim = append(slim, out[len(out)-1])
		out = slim
	}
	return out, rows.Err()
}
// PruneMetrics deletes samples older than the retention window (default 7d).
func (s *Store) PruneMetrics(retention time.Duration) {
	cutoff := time.Now().Add(-retention).Unix()
	_, _ = s.DB.Exec(`DELETE FROM node_metrics WHERE ts < ?`, cutoff)
}
