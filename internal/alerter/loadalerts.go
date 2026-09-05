package alerter

import (
	"strconv"

	"github.com/shirou/gopsutil/v4/cpu"
	"github.com/shirou/gopsutil/v4/disk"
	"github.com/shirou/gopsutil/v4/mem"
)

// resourceAlerts evaluates local CPU/RAM/disk thresholds via gopsutil
// (cross-platform, already a dependency). cpuMin/ramMin/diskMin are
// percent thresholds; 0 disables the check.
func resourceAlerts(a *Alerter, cpuMin, ramMin, diskMin int) {
	if ramMin > 0 {
		if vm, err := mem.VirtualMemory(); err == nil {
			if int(vm.UsedPercent) >= ramMin {
				a.fire("warning", "system", "High memory usage",
					"Memory usage is at "+strconv.Itoa(int(vm.UsedPercent))+"% (threshold "+strconv.Itoa(ramMin)+"%).",
					"localhost", "local-ram-high")
			}
		}
	}
	if diskMin > 0 {
		if du, err := disk.Usage("/"); err == nil {
			if int(du.UsedPercent) >= diskMin {
				a.fire("warning", "system", "High disk usage",
					"Root filesystem usage is at "+strconv.Itoa(int(du.UsedPercent))+"% (threshold "+strconv.Itoa(diskMin)+"%).",
					"localhost", "local-disk-high")
			}
		}
	}
	if cpuMin > 0 {
		if pct, err := cpu.Percent(0, false); err == nil && len(pct) > 0 {
			if int(pct[0]) >= cpuMin {
				a.fire("warning", "system", "High CPU usage",
					"CPU usage is at "+strconv.Itoa(int(pct[0]))+"% (threshold "+strconv.Itoa(cpuMin)+"%).",
					"localhost", "local-cpu-high")
			}
		}
	}
}
