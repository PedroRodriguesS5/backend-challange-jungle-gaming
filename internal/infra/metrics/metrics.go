package metrics

import (
	"sync/atomic"
)

type Metrics struct {
	StatusProcessed           atomic.Int64 `json:"statusProcessed"`
	StatusRejected            atomic.Int64 `json:"statusRejected"`
	StatusPendingReference    atomic.Int64 `json:"statusPendingReference"`
	StatusFailed              atomic.Int64 `json:"statusFailed"`
	Duplicates                atomic.Int64 `json:"duplicates"`
	Retries                   atomic.Int64 `json:"retries"`
	DLQ                       atomic.Int64 `json:"dlq"`
	Conflicts                 atomic.Int64 `json:"conflicts"`
	ReconciliationDivergences atomic.Int64 `json:"reconciliationDivergences"`
}

var Default = &Metrics{}

func IncStatus(status string) {
	switch status {
	case "PROCESSED":
		Default.StatusProcessed.Add(1)
	case "REJECTED":
		Default.StatusRejected.Add(1)
	case "PENDING_REFERENCE":
		Default.StatusPendingReference.Add(1)
	case "FAILED":
		Default.StatusFailed.Add(1)
	}
}

func IncDuplicates() {
	Default.Duplicates.Add(1)
}

func IncRetries() {
	Default.Retries.Add(1)
}

func IncDLQ() {
	Default.DLQ.Add(1)
}

func IncConflicts() {
	Default.Conflicts.Add(1)
}

func IncReconciliationDivergences() {
	Default.ReconciliationDivergences.Add(1)
}

func Snapshot() map[string]int64 {
	return map[string]int64{
		"statusProcessed":           Default.StatusProcessed.Load(),
		"statusRejected":            Default.StatusRejected.Load(),
		"statusPendingReference":    Default.StatusPendingReference.Load(),
		"statusFailed":              Default.StatusFailed.Load(),
		"duplicates":                Default.Duplicates.Load(),
		"retries":                   Default.Retries.Load(),
		"dlq":                       Default.DLQ.Load(),
		"conflicts":                 Default.Conflicts.Load(),
		"reconciliationDivergences": Default.ReconciliationDivergences.Load(),
	}
}
