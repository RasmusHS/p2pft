package transfer

// Receiver → Sender, first message on the direct connection.
type ResumeRequest struct {
	Offset int64 `json:"offset"` // 0 = fresh, >0 = resume from this byte
}

// Sender → Receiver, acknowledging.
type TransferStart struct {
	StartOffset int64 `json:"start_offset"`
}
