package syncengine

import "testing"

func TestV1RefundSyncLostAckRestartAndOrderingAcceptance(t *testing.T) {
	t.Run("lost acknowledgement survives restart", TestRefundEventRecoversWhenCentralAcceptsButAcknowledgementIsLost)
	t.Run("same-order refund facts stay blocked until recovery", TestRefundOrderingKeyBlocksLaterFactsUntilLostAckEventRecovers)
}
