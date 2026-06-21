//go:build !linux

package api

func diskStats() (totalGB, usedGB, usedPct float64) { return 0, 0, 0 }
func netStats() (rxMBps, txMBps, rxTotalMB, txTotalMB float64) { return 0, 0, 0, 0 }
