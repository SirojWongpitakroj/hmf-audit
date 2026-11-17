package canonicalingestion

import "sync/atomic"

// Fixed-size attempted-work counters. Timers can nest: e.g. SourceCheckNanos is
// inside PlanningNanos, and queue drains can be inside sealing/checkpoint work.
type IngestionPhaseMetrics struct {
	PlanningCalls, PlanningNanos, SchedulerNanos                                           atomic.Uint64
	SourceCheckCalls, SourceCheckIDs, SourceCheckNanos                                     atomic.Uint64
	PendingLoadNanos, PlanWriteCalls, PlanWriteNanos                                       atomic.Uint64
	PlanLockNanos, PlanEncodeNanos, PlanInsertNanos, PlanCursorNanos, PlanCommitNanos      atomic.Uint64
	PlanInsertCalls, PlanRowsAttempted, PlansAcknowledged                                  atomic.Uint64
	JournalCompleteNanos                                                                   atomic.Uint64
	PublicationPlanOverlapCalls, PublicationPlanBoundaryNanos, PublicationBarrierWaitNanos atomic.Uint64
	BackgroundPublicationCalls, BackgroundPublicationFailures                              atomic.Uint64
	PublicationIngestionEpochs, PublicationIngestionOverlapNanos                           atomic.Uint64
	PublicationFinalRecordWaitNanos, PublicationIngestionDrainNanos                        atomic.Uint64
	PlanPrecomputeCalls, PlanPrecomputeHits, PlanPrecomputeFallbacks                       atomic.Uint64
	PlanPrecomputeWaitNanos                                                                atomic.Uint64
	AdmissionCalls, AdmissionNanos, AdmissionLockNanos, AdmissionWriteNanos                atomic.Uint64
	AdmissionCommitNanos, AdmissionWaitNanos, AdmissionAheadCalls                          atomic.Uint64
	PrepareCalls, PrepareNanos                                                             atomic.Uint64
	FullFinalizationCalls, FullFinalizationNanos, SealingNanos                             atomic.Uint64
	HMFSealNanos, SealedEvidenceNanos, EvidenceDrainCalls, EvidenceDrainNanos              atomic.Uint64
}
type IngestionPhaseSnapshot struct {
	PlanningCalls, PlanningNanos, SchedulerNanos                                           uint64
	SourceCheckCalls, SourceCheckIDs, SourceCheckNanos                                     uint64
	PendingLoadNanos, PlanWriteCalls, PlanWriteNanos                                       uint64
	PlanLockNanos, PlanEncodeNanos, PlanInsertNanos, PlanCursorNanos, PlanCommitNanos      uint64
	PlanInsertCalls, PlanRowsAttempted, PlansAcknowledged                                  uint64
	JournalCompleteNanos                                                                   uint64
	PublicationPlanOverlapCalls, PublicationPlanBoundaryNanos, PublicationBarrierWaitNanos uint64
	BackgroundPublicationCalls, BackgroundPublicationFailures                              uint64
	PublicationIngestionEpochs, PublicationIngestionOverlapNanos                           uint64
	PublicationFinalRecordWaitNanos, PublicationIngestionDrainNanos                        uint64
	PlanPrecomputeCalls, PlanPrecomputeHits, PlanPrecomputeFallbacks                       uint64
	PlanPrecomputeWaitNanos                                                                uint64
	AdmissionCalls, AdmissionNanos, AdmissionLockNanos, AdmissionWriteNanos                uint64
	AdmissionCommitNanos, AdmissionWaitNanos, AdmissionAheadCalls                          uint64
	PrepareCalls, PrepareNanos                                                             uint64
	FullFinalizationCalls, FullFinalizationNanos, SealingNanos                             uint64
	HMFSealNanos, SealedEvidenceNanos, EvidenceDrainCalls, EvidenceDrainNanos              uint64
}

func (m *IngestionPhaseMetrics) Snapshot() IngestionPhaseSnapshot {
	return IngestionPhaseSnapshot{
		PlanningCalls: m.PlanningCalls.Load(), PlanningNanos: m.PlanningNanos.Load(), SchedulerNanos: m.SchedulerNanos.Load(),
		SourceCheckCalls: m.SourceCheckCalls.Load(), SourceCheckIDs: m.SourceCheckIDs.Load(), SourceCheckNanos: m.SourceCheckNanos.Load(),
		PendingLoadNanos: m.PendingLoadNanos.Load(), PlanWriteCalls: m.PlanWriteCalls.Load(), PlanWriteNanos: m.PlanWriteNanos.Load(),
		PlanLockNanos: m.PlanLockNanos.Load(), PlanEncodeNanos: m.PlanEncodeNanos.Load(), PlanInsertNanos: m.PlanInsertNanos.Load(), PlanCursorNanos: m.PlanCursorNanos.Load(), PlanCommitNanos: m.PlanCommitNanos.Load(),
		PlanInsertCalls: m.PlanInsertCalls.Load(), PlanRowsAttempted: m.PlanRowsAttempted.Load(), PlansAcknowledged: m.PlansAcknowledged.Load(), JournalCompleteNanos: m.JournalCompleteNanos.Load(),
		PublicationPlanOverlapCalls: m.PublicationPlanOverlapCalls.Load(), PublicationPlanBoundaryNanos: m.PublicationPlanBoundaryNanos.Load(), PublicationBarrierWaitNanos: m.PublicationBarrierWaitNanos.Load(),
		BackgroundPublicationCalls: m.BackgroundPublicationCalls.Load(), BackgroundPublicationFailures: m.BackgroundPublicationFailures.Load(), PublicationIngestionEpochs: m.PublicationIngestionEpochs.Load(), PublicationIngestionOverlapNanos: m.PublicationIngestionOverlapNanos.Load(), PublicationFinalRecordWaitNanos: m.PublicationFinalRecordWaitNanos.Load(), PublicationIngestionDrainNanos: m.PublicationIngestionDrainNanos.Load(),
		PlanPrecomputeCalls: m.PlanPrecomputeCalls.Load(), PlanPrecomputeHits: m.PlanPrecomputeHits.Load(), PlanPrecomputeFallbacks: m.PlanPrecomputeFallbacks.Load(), PlanPrecomputeWaitNanos: m.PlanPrecomputeWaitNanos.Load(),
		AdmissionCalls: m.AdmissionCalls.Load(), AdmissionNanos: m.AdmissionNanos.Load(), AdmissionLockNanos: m.AdmissionLockNanos.Load(), AdmissionWriteNanos: m.AdmissionWriteNanos.Load(),
		AdmissionCommitNanos: m.AdmissionCommitNanos.Load(), AdmissionWaitNanos: m.AdmissionWaitNanos.Load(), AdmissionAheadCalls: m.AdmissionAheadCalls.Load(),
		PrepareCalls: m.PrepareCalls.Load(), PrepareNanos: m.PrepareNanos.Load(),
		FullFinalizationCalls: m.FullFinalizationCalls.Load(), FullFinalizationNanos: m.FullFinalizationNanos.Load(), SealingNanos: m.SealingNanos.Load(), HMFSealNanos: m.HMFSealNanos.Load(), SealedEvidenceNanos: m.SealedEvidenceNanos.Load(), EvidenceDrainCalls: m.EvidenceDrainCalls.Load(), EvidenceDrainNanos: m.EvidenceDrainNanos.Load(),
	}
}
