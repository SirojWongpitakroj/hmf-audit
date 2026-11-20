package eval

import "os"

// Experiment 1 on the native query set (systems/experiments/native-queries):
// the queries are read by fairBLASNativeScenarios (fair_native_queries_test.go),
// which understands the keyword_terms / record_ordinals file format and checks
// the returned timestamps. FAIR_NATIVE_QUERIES_DIR selects the set directory
// (FAIR_QUERY_DIR is accepted as the older name); unset keeps the
// clustered-2/scattered batch keywords.

const fairNativeWorkloadVersion = 9

func fairNativeQueriesDir() string {
	if dir := os.Getenv("FAIR_NATIVE_QUERIES_DIR"); dir != "" {
		return dir
	}
	return os.Getenv("FAIR_QUERY_DIR")
}
