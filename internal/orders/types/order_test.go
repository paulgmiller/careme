package types

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTransferStatusRoundTrip(t *testing.T) {
	for _, status := range []TransferStatus{TransferPending, TransferFailed, TransferApplied} {
		t.Run(string(status), func(t *testing.T) {
			original := Transfer{TransferID: "list", Status: status, CartURL: "https://www.qfc.com/cart", Items: []TransferItem{{ProductID: "food", AcceptedQuantity: 2, RejectedQuantity: 1, RejectionReason: "insufficient_stock"}}}
			body, err := json.Marshal(original)
			require.NoError(t, err)
			var decoded Transfer
			require.NoError(t, json.Unmarshal(body, &decoded))
			assert.Equal(t, original, decoded)
			assert.NotContains(t, string(body), `"code"`)
		})
	}
}
