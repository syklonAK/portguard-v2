package sysinfo

import (
	"context"
	"runtime"
	"time"

	"github.com/shirou/gopsutil/v4/cpu"
	"github.com/shirou/gopsutil/v4/disk"
	"github.com/shirou/gopsutil/v4/host"
	"github.com/shirou/gopsutil/v4/load"
	"github.com/shirou/gopsutil/v4/mem"
	"github.com/shirou/gopsutil/v4/net"
)

type Snapshot struct {
	CPUPercent   float64 `json:"cpu_percent"`
	MemTotal     uint64  `json:"mem_total"`
	MemUsed      uint64  `json:"mem_used"`
	MemPercent   float64 `json:"mem_percent"`
	DiskTotal    uint64  `json:"disk_total"`
	DiskUsed     uint64  `json:"disk_used"`
	DiskPercent  float64 `json:"disk_percent"`
	Load1        float64 `json:"load1"`
	Uptime       uint64  `json:"uptime"`
	OS           string  `json:"os"`
	Platform     string  `json:"platform"`
	Kernel       string  `json:"kernel"`
	NumCPU       int     `json:"num_cpu"`
	GoVersion    string  `json:"go_version"`
	// cumulative network counters (bytes) across all interfaces
	NetRxTotal uint64 `json:"net_rx_total"`
	NetTxTotal uint64 `json:"net_tx_total"`
}

func SnapshotNow() Snapshot {
	s := Snapshot{GoVersion: runtime.Version(), NumCPU: runtime.NumCPU()}
	if vm, err := mem.VirtualMemory(); err == nil {
		s.MemTotal = vm.Total
		s.MemUsed = vm.Used
		s.MemPercent = vm.UsedPercent
	}
	if du, err := disk.Usage("/"); err == nil {
		s.DiskTotal = du.Total
		s.DiskUsed = du.Used
		s.DiskPercent = du.UsedPercent
	}
	if pct, err := cpu.Percent(time.Millisecond*100, false); err == nil && len(pct) > 0 {
		s.CPUPercent = pct[0]
	}
	if lv, err := load.Avg(); err == nil {
		s.Load1 = lv.Load1
	}
	if hi, err := host.Info(); err == nil {
		s.Uptime = hi.Uptime
		s.OS = hi.OS
		s.Platform = hi.Platform + " " + hi.PlatformVersion
		s.Kernel = hi.KernelVersion
	}
	if counters, err := net.IOCounters(false); err == nil && len(counters) > 0 {
		// aggregate ("false") gives one row with totals across interfaces
		s.NetRxTotal = counters[0].BytesRecv
		s.NetTxTotal = counters[0].BytesSent
	}
	return s
}

// RunSampler periodically pushes snapshots to the broker.
func RunSampler(ctx context.Context, interval time.Duration, sink func(Snapshot)) {
	if interval < 2*time.Second {
		interval = 2 * time.Second
	}
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			sink(SnapshotNow())
		}
	}
}
