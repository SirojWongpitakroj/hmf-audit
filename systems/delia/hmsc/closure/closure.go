// Package closure implements CLOSE-008's audit-closure coupling
// (DEV-08/SYS-007): "no separate log-collector/preprocessor/verification
// module exists — stopping the log reader and LSG is coupled with invoking
// Close." No separate module teardown step exists in this reproduction.
//
// The rest of the Close/Disassociate client flow (building and sending
// CloseMsg/DisassociateMsg, collecting the required signatures) is exercised
// directly against contracts/HMSC.sol by the Hardhat/JS test suite
// (test/close.test.js, test/disassociate.test.js); this package only
// couples the off-chain log-reader lifecycle to that invocation, per
// SYS-007/DEV-08's specific requirement.
package closure

// Stopper is the minimal interface CloseInstance needs from a log reader
// (lsg.LogReader satisfies it via its own Stop method; kept as an interface
// here so this coupling is testable without starting a real reader in every
// test).
type Stopper interface {
	Stop()
}

// CloseInstance couples "enable"/"close" with Init/Close (SYS-007, DEV-08):
// it stops reader FIRST, unconditionally, then invokes invokeClose (the
// caller's own Close-protocol client call — building/sending CloseMsg,
// exercised against the real contract elsewhere). The reader is always
// stopped exactly once, regardless of whether invokeClose succeeds; there
// is no separate module to tear down afterward.
func CloseInstance(reader Stopper, invokeClose func() error) error {
	reader.Stop()
	return invokeClose()
}
