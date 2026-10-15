package main

import (
	"trove.local/phase0/internal/native"
	"trove.local/phase0/internal/workspace"
)

// Ephemeral acquisition state belongs to the application, not a settings view.
// Durable partial manifests remain the restart/resume source after process exit.
type modelOperation struct {
	Version       int    `json:"version"`
	Revision      uint64 `json:"revision"`
	AcquisitionID string `json:"acquisitionId,omitempty"`
	PackID        string `json:"packId"`
	State         string `json:"state"`
	Bytes         int64  `json:"bytes"`
	Error         string `json:"error,omitempty"`
}

func (n *nativeState) startModelOperationLocked(id, state string) modelOperation {
	if n.operations == nil {
		n.operations = map[string]modelOperation{}
	}
	n.operationRevision++
	result := modelOperation{Version: 1, Revision: n.operationRevision, AcquisitionID: workspace.NewID(), PackID: id, State: state}
	n.operations[id] = result
	return result
}
func (n *nativeState) updateModelOperation(id, state string, bytes int64, failure string) modelOperation {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.updateModelOperationLocked(id, state, bytes, failure)
}
func (n *nativeState) updateModelOperationLocked(id, state string, bytes int64, failure string) modelOperation {
	if n.operations == nil {
		n.operations = map[string]modelOperation{}
	}
	result := n.operations[id]
	n.operationRevision++
	result.Version, result.PackID, result.Revision = 1, id, n.operationRevision
	if result.State == "cancellation_requested" && (state == "acquiring" || state == "importing") {
		state = result.State
	}
	result.State, result.Error = state, failure
	if bytes >= 0 {
		result.Bytes = bytes
	}
	if state == "ready" || state == "failed" || state == "interrupted" || state == "removed" {
		delete(n.cancellations, id)
	}
	n.operations[id] = result
	return result
}
func (n *nativeState) cancelModelOperation(id string) (modelOperation, bool) {
	n.mu.Lock()
	defer n.mu.Unlock()
	result, ok := n.operations[id]
	if cancel := n.cancellations[id]; cancel != nil {
		n.operationRevision++
		result.Version, result.Revision, result.PackID, result.State = 1, n.operationRevision, id, "cancellation_requested"
		n.operations[id] = result
		cancel()
		return result, true
	}
	// Completion may win the race with a click on Cancel. Return its newer state.
	return result, ok
}
func (n *nativeState) modelOperations(packs []native.PackStatus, observedRevision uint64) []modelOperation {
	n.mu.Lock()
	defer n.mu.Unlock()
	result := make([]modelOperation, 0, len(n.operations))
	for id, op := range n.operations {
		// A previous successful transfer cannot override fresh on-disk
		// verification (for example a missing or damaged installed file).
		if (op.State == "ready" || op.State == "corrupt" || op.State == "removed") && n.cancellations[id] == nil && op.Revision <= observedRevision {
			actual := "removed"
			for _, pack := range packs {
				if pack.Pack.ID == id {
					actual = pack.State
					break
				}
			}
			if actual != op.State {
				op = n.updateModelOperationLocked(id, actual, -1, "")
			}
		}
		result = append(result, op)
	}
	return result
}
