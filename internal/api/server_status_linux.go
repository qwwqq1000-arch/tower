package api

import (
	"bufio"
	"os"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

var netMu sync.Mutex
var lastNetSample struct {
	rx, tx uint64
	t      time.Time
}

func diskStats() (totalGB, usedGB, usedPct float64) {
	var st syscall.Statfs_t
	if err := syscall.Statfs("/", &st); err != nil {
		return 0, 0, 0
	}
	total := st.Blocks * uint64(st.Bsize)
	free := st.Bavail * uint64(st.Bsize)
	used := total - free
	if total == 0 {
		return 0, 0, 0
	}
	totalGB = float64(total) / 1024 / 1024 / 1024
	usedGB = float64(used) / 1024 / 1024 / 1024
	usedPct = float64(used) / float64(total) * 100
	return
}

func readNetDev() (rx, tx uint64) {
	f, err := os.Open("/proc/net/dev")
	if err != nil {
		return
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	// skip 2 header lines
	sc.Scan()
	sc.Scan()
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		colonIdx := strings.Index(line, ":")
		if colonIdx < 0 {
			continue
		}
		iface := strings.TrimSpace(line[:colonIdx])
		if iface == "lo" {
			continue
		}
		fields := strings.Fields(line[colonIdx+1:])
		if len(fields) < 9 {
			continue
		}
		r, _ := strconv.ParseUint(fields[0], 10, 64)
		t, _ := strconv.ParseUint(fields[8], 10, 64)
		rx += r
		tx += t
	}
	return
}

func netStats() (rxMBps, txMBps, rxTotalMB, txTotalMB float64) {
	curRx, curTx := readNetDev()
	rxTotalMB = float64(curRx) / 1024 / 1024
	txTotalMB = float64(curTx) / 1024 / 1024

	netMu.Lock()
	defer netMu.Unlock()

	now := time.Now()
	if !lastNetSample.t.IsZero() {
		elapsed := now.Sub(lastNetSample.t).Seconds()
		if elapsed > 0 {
			rxMBps = float64(curRx-lastNetSample.rx) / elapsed / 1024 / 1024
			txMBps = float64(curTx-lastNetSample.tx) / elapsed / 1024 / 1024
		}
	}
	lastNetSample.rx = curRx
	lastNetSample.tx = curTx
	lastNetSample.t = now
	return
}
