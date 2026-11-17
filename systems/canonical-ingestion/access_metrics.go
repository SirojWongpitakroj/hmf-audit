package canonicalingestion

import "sync/atomic"

// Fixed-size counters: no query text, record IDs or historical samples retained.
type AccessMetrics struct {
	IngestionPhases                  IngestionPhaseMetrics
	PublicationWork                  PublicationMetrics
	LocatorWork                      LocatorWorkMetrics
	SplitEntrySizes                  atomic.Uint64
	PageCopySites                    PageCopySites
	PageTransfers, PageTransferBytes atomic.Uint64
	// Cleanup measures attempted SQL work; acknowledged rows may later roll back.
	CleanupCalls, CleanupSQLCalls, CleanupRowsDeleted, CleanupPageRowsDeleted, CleanupLeafRowsDeleted                                      atomic.Uint64
	CleanupNanos, CleanupPagesNanos, CleanupLeavesNanos, CleanupTreesRequested                                                             atomic.Uint64
	CleanupSegment, CleanupGeneration                                                                                                      atomic.Uint64
	TransactionCommitCalls, TransactionCommitNanos, TransactionCommitFailures                                                              atomic.Uint64
	IndexPageRows, IndexLeafRows, IndexPayloadBytes, FileIndexRows, BlockIndexRows, SegmentIndexRows                                       atomic.Uint64
	IndexFlushes, IndexInsertCalls, IndexFlushNanos, IndexFinishFlushes, IndexCapacityFlushes, IndexBarrierFlushes, IndexFinishFlushesHeld atomic.Uint64
	PeakIndexBatchRows, PeakIndexBatchBytes                                                                                                atomic.Int64
	SegmentPositions                                                                                                                       [6]SegmentPositionCounters
	SegmentHashRequests, SegmentHashHits, SegmentHashComputations, SegmentHashedBytes, SegmentHashNanos                                    atomic.Uint64
	SegmentHashCacheBytes, PeakSegmentHashCacheBytes                                                                                       atomic.Int64
	SQLReadCalls, SQLWriteCalls, SQLStatementNanos                                                                                         atomic.Uint64
	TransactionObjectInsertCalls, TransactionObjectRows                                                                                    atomic.Uint64
	EntryObjectReadRows, EntryKeywordReadRows, EntryObjectRereadsAvoided, EntryKeywordRereadsAvoided                                       atomic.Uint64
	FileClosures, ClosedFileEntries, TimestampReversalClosures, ObjectBuildNanos                                                           atomic.Uint64
	KeywordEncodingsReused, ZeroBitmapComputationsReused                                                                                   atomic.Uint64
	PeakFileInputBytes, PeakTransactionObjectBytes                                                                                         atomic.Int64
	PublicationGateWaitNanos, ProducerGateWaitNanos, PublicationRecords, PublicationBatches                                                atomic.Uint64
	SegmentRecordsCopied, PersistedSegmentsSkipped, SegmentRecordsCopyAvoided                                                              atomic.Uint64
	SplitEntrySerializations, SplitCandidates, SplitNanos                                                                                  atomic.Uint64
	HPBFSnapshotsAllocated, HPBFSnapshotsReused, HPBFSnapshotBytes                                                                         atomic.Uint64
	PageCopies, PageCopyBytes                                                                                                              atomic.Uint64
	MPMNanos, MVAOperationNanos, HPBFOperationNanos, CheckpointNanos, PublicationNanos                                                     atomic.Uint64
	ActiveMetadataReads, ActiveMetadataReadsAvoided                                                                                        atomic.Uint64
	HPBFWritesAttempted, HPBFWritesSkipped                                                                                                 atomic.Uint64
	SealedNodeRows, SealedNodeRequests                                                                                                     atomic.Uint64
	KeywordMetadataQueries, TimestampQueries, FileMetadataQueries                                                                          atomic.Uint64
	BitmapBuildNanos, BlockBuildNanos                                                                                                      atomic.Uint64
	SummaryCacheHits                                                                                                                       atomic.Uint64
	PeakSummaryBytes                                                                                                                       atomic.Int64
}
type AccessSnapshot struct {
	IngestionPhases                                                                                                                        IngestionPhaseSnapshot
	PublicationWork                                                                                                                        PublicationSnapshot
	LocatorWork                                                                                                                            LocatorWorkSnapshot
	SplitEntrySizes                                                                                                                        uint64
	PageCopySites                                                                                                                          PageCopySitesSnapshot
	PageTransfers, PageTransferBytes                                                                                                       uint64
	CleanupCalls, CleanupSQLCalls, CleanupRowsDeleted, CleanupPageRowsDeleted, CleanupLeafRowsDeleted                                      uint64
	CleanupNanos, CleanupPagesNanos, CleanupLeavesNanos, CleanupTreesRequested                                                             uint64
	CleanupSegment, CleanupGeneration                                                                                                      uint64
	TransactionCommitCalls, TransactionCommitNanos, TransactionCommitFailures                                                              uint64
	IndexPageRows, IndexLeafRows, IndexPayloadBytes, FileIndexRows, BlockIndexRows, SegmentIndexRows                                       uint64
	IndexFlushes, IndexInsertCalls, IndexFlushNanos, IndexFinishFlushes, IndexCapacityFlushes, IndexBarrierFlushes, IndexFinishFlushesHeld uint64
	PeakIndexBatchRows, PeakIndexBatchBytes                                                                                                int64
	SegmentPositions                                                                                                                       [6]SegmentPositionSnapshot
	SegmentHashRequests, SegmentHashHits, SegmentHashComputations, SegmentHashedBytes, SegmentHashNanos                                    uint64
	SegmentHashCacheBytes, PeakSegmentHashCacheBytes                                                                                       int64
	SQLReadCalls, SQLWriteCalls, SQLStatementNanos                                                                                         uint64
	TransactionObjectInsertCalls, TransactionObjectRows                                                                                    uint64
	EntryObjectReadRows, EntryKeywordReadRows, EntryObjectRereadsAvoided, EntryKeywordRereadsAvoided                                       uint64
	FileClosures, ClosedFileEntries, TimestampReversalClosures, ObjectBuildNanos                                                           uint64
	KeywordEncodingsReused, ZeroBitmapComputationsReused                                                                                   uint64
	PeakFileInputBytes, PeakTransactionObjectBytes                                                                                         int64
	PublicationGateWaitNanos, ProducerGateWaitNanos, PublicationRecords, PublicationBatches                                                uint64
	SegmentRecordsCopied, PersistedSegmentsSkipped, SegmentRecordsCopyAvoided                                                              uint64
	SplitEntrySerializations, SplitCandidates, SplitNanos                                                                                  uint64
	HPBFSnapshotsAllocated, HPBFSnapshotsReused, HPBFSnapshotBytes                                                                         uint64
	PageCopies, PageCopyBytes                                                                                                              uint64
	MPMNanos, MVAOperationNanos, HPBFOperationNanos, CheckpointNanos, PublicationNanos                                                     uint64
	ActiveMetadataReads, ActiveMetadataReadsAvoided                                                                                        uint64
	HPBFWritesAttempted, HPBFWritesSkipped                                                                                                 uint64
	SealedNodeRows, SealedNodeRequests                                                                                                     uint64
	KeywordMetadataQueries, TimestampQueries, FileMetadataQueries                                                                          uint64
	BitmapBuildNanos, BlockBuildNanos, SummaryCacheHits                                                                                    uint64
	PeakSummaryBytes                                                                                                                       int64
}

func (m *AccessMetrics) Snapshot() AccessSnapshot {
	return AccessSnapshot{
		IngestionPhases: m.IngestionPhases.Snapshot(),
		PublicationWork: m.PublicationWork.Snapshot(),
		LocatorWork:     m.LocatorWork.Snapshot(),
		SplitEntrySizes: m.SplitEntrySizes.Load(),
		PageCopySites:   m.PageCopySites.Snapshot(), PageTransfers: m.PageTransfers.Load(), PageTransferBytes: m.PageTransferBytes.Load(),
		CleanupCalls: m.CleanupCalls.Load(), CleanupSQLCalls: m.CleanupSQLCalls.Load(), CleanupRowsDeleted: m.CleanupRowsDeleted.Load(), CleanupPageRowsDeleted: m.CleanupPageRowsDeleted.Load(), CleanupLeafRowsDeleted: m.CleanupLeafRowsDeleted.Load(),
		CleanupNanos: m.CleanupNanos.Load(), CleanupPagesNanos: m.CleanupPagesNanos.Load(), CleanupLeavesNanos: m.CleanupLeavesNanos.Load(), CleanupTreesRequested: m.CleanupTreesRequested.Load(), CleanupSegment: m.CleanupSegment.Load(), CleanupGeneration: m.CleanupGeneration.Load(),
		TransactionCommitCalls: m.TransactionCommitCalls.Load(), TransactionCommitNanos: m.TransactionCommitNanos.Load(), TransactionCommitFailures: m.TransactionCommitFailures.Load(),
		IndexPageRows: m.IndexPageRows.Load(), IndexLeafRows: m.IndexLeafRows.Load(), IndexPayloadBytes: m.IndexPayloadBytes.Load(), FileIndexRows: m.FileIndexRows.Load(), BlockIndexRows: m.BlockIndexRows.Load(), SegmentIndexRows: m.SegmentIndexRows.Load(),
		IndexFlushes: m.IndexFlushes.Load(), IndexInsertCalls: m.IndexInsertCalls.Load(), IndexFlushNanos: m.IndexFlushNanos.Load(), IndexFinishFlushes: m.IndexFinishFlushes.Load(), IndexCapacityFlushes: m.IndexCapacityFlushes.Load(), IndexBarrierFlushes: m.IndexBarrierFlushes.Load(), IndexFinishFlushesHeld: m.IndexFinishFlushesHeld.Load(), PeakIndexBatchRows: m.PeakIndexBatchRows.Load(), PeakIndexBatchBytes: m.PeakIndexBatchBytes.Load(),
		SegmentPositions:    m.segmentPositions(),
		SegmentHashRequests: m.SegmentHashRequests.Load(), SegmentHashHits: m.SegmentHashHits.Load(), SegmentHashComputations: m.SegmentHashComputations.Load(), SegmentHashedBytes: m.SegmentHashedBytes.Load(), SegmentHashNanos: m.SegmentHashNanos.Load(), SegmentHashCacheBytes: m.SegmentHashCacheBytes.Load(), PeakSegmentHashCacheBytes: m.PeakSegmentHashCacheBytes.Load(),
		SQLReadCalls: m.SQLReadCalls.Load(), SQLWriteCalls: m.SQLWriteCalls.Load(), SQLStatementNanos: m.SQLStatementNanos.Load(),
		TransactionObjectInsertCalls: m.TransactionObjectInsertCalls.Load(), TransactionObjectRows: m.TransactionObjectRows.Load(),
		EntryObjectReadRows: m.EntryObjectReadRows.Load(), EntryKeywordReadRows: m.EntryKeywordReadRows.Load(), EntryObjectRereadsAvoided: m.EntryObjectRereadsAvoided.Load(), EntryKeywordRereadsAvoided: m.EntryKeywordRereadsAvoided.Load(),
		FileClosures: m.FileClosures.Load(), ClosedFileEntries: m.ClosedFileEntries.Load(), TimestampReversalClosures: m.TimestampReversalClosures.Load(), ObjectBuildNanos: m.ObjectBuildNanos.Load(),
		KeywordEncodingsReused: m.KeywordEncodingsReused.Load(), ZeroBitmapComputationsReused: m.ZeroBitmapComputationsReused.Load(), PeakFileInputBytes: m.PeakFileInputBytes.Load(), PeakTransactionObjectBytes: m.PeakTransactionObjectBytes.Load(),
		PublicationGateWaitNanos: m.PublicationGateWaitNanos.Load(), ProducerGateWaitNanos: m.ProducerGateWaitNanos.Load(), PublicationRecords: m.PublicationRecords.Load(), PublicationBatches: m.PublicationBatches.Load(),
		SegmentRecordsCopied: m.SegmentRecordsCopied.Load(), PersistedSegmentsSkipped: m.PersistedSegmentsSkipped.Load(), SegmentRecordsCopyAvoided: m.SegmentRecordsCopyAvoided.Load(),
		SplitEntrySerializations: m.SplitEntrySerializations.Load(), SplitCandidates: m.SplitCandidates.Load(), SplitNanos: m.SplitNanos.Load(),
		HPBFSnapshotsAllocated: m.HPBFSnapshotsAllocated.Load(), HPBFSnapshotsReused: m.HPBFSnapshotsReused.Load(), HPBFSnapshotBytes: m.HPBFSnapshotBytes.Load(), PageCopies: m.PageCopies.Load(), PageCopyBytes: m.PageCopyBytes.Load(),
		MPMNanos: m.MPMNanos.Load(), MVAOperationNanos: m.MVAOperationNanos.Load(), HPBFOperationNanos: m.HPBFOperationNanos.Load(), CheckpointNanos: m.CheckpointNanos.Load(), PublicationNanos: m.PublicationNanos.Load(),
		ActiveMetadataReads: m.ActiveMetadataReads.Load(), ActiveMetadataReadsAvoided: m.ActiveMetadataReadsAvoided.Load(), HPBFWritesAttempted: m.HPBFWritesAttempted.Load(), HPBFWritesSkipped: m.HPBFWritesSkipped.Load(), SealedNodeRows: m.SealedNodeRows.Load(), SealedNodeRequests: m.SealedNodeRequests.Load(), KeywordMetadataQueries: m.KeywordMetadataQueries.Load(), TimestampQueries: m.TimestampQueries.Load(), FileMetadataQueries: m.FileMetadataQueries.Load(), BitmapBuildNanos: m.BitmapBuildNanos.Load(), BlockBuildNanos: m.BlockBuildNanos.Load(), SummaryCacheHits: m.SummaryCacheHits.Load(), PeakSummaryBytes: m.PeakSummaryBytes.Load(),
	}
}
func (m *AccessMetrics) SummaryBytes(n int64) { peak(&m.PeakSummaryBytes, n) }
