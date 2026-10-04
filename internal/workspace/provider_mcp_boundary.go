package workspace

import (
	"bytes"
	"encoding/json"
	"io"
	"math/big"
	"net/http"
)

// The pinned SDK decodes numeric request/cancellation IDs through float64.
// Reject integers that cannot round-trip exactly rather than conflating calls.
// The stdio relay losslessly maps its IDs to strings at this HTTP boundary.
func validateMCPHTTPIDs(w http.ResponseWriter, r *http.Request) bool {
	if r.Method != http.MethodPost {
		return true
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, (1<<20)+1))
	_ = r.Body.Close()
	if err != nil || len(body) > 1<<20 {
		http.Error(w, "Request exceeds one MiB or is incomplete", http.StatusRequestEntityTooLarge)
		return false
	}
	r.Body = io.NopCloser(bytes.NewReader(body))
	var request struct {
		ID     json.RawMessage `json:"id"`
		Method string          `json:"method"`
		Params struct {
			RequestID json.RawMessage `json:"requestId"`
		} `json:"params"`
	}
	// Protocol syntax and unknown fields remain the SDK's responsibility.
	if json.Unmarshal(body, &request) != nil {
		return true
	}
	safe := func(raw json.RawMessage) bool {
		raw = bytes.TrimSpace(raw)
		if len(raw) == 0 || raw[0] == '"' {
			return true
		}
		if len(raw) > 17 {
			return false
		}
		n, ok := new(big.Int).SetString(string(raw), 10)
		if !ok {
			return false
		}
		return n.Abs(n).Cmp(big.NewInt(9007199254740991)) <= 0
	}
	if !safe(request.ID) || (request.Method == "notifications/cancelled" && !safe(request.Params.RequestID)) {
		http.Error(w, "Use a string request ID or an integer within ±9007199254740991", http.StatusBadRequest)
		return false
	}
	return true
}
