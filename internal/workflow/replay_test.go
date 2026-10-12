package workflow

import (
	"bytes"
	"testing"

	"go.temporal.io/sdk/worker"
)

// replayTestKey encrypted testdata/sync_provider_history.json. It is a
// public test-only key and must never be used outside tests.
var replayTestKey = bytes.Repeat([]byte{0x01}, 32)

// TestReplaySyncProviderHistory replays a history recorded from a real
// Temporal server (Lite profile, payload key "k2" = replayTestKey). It fails
// when a code change makes SyncProviderWorkflow non-deterministic for runs
// already in flight; such changes need workflow.GetVersion (roadmap §3.8).
//
// To refresh the recording after an intentional, versioned change, capture a
// new history with `temporal workflow show -o json` from a worker running with
// SEMAR_PAYLOAD_KEY_ID=k2 and replayTestKey.
func TestReplaySyncProviderHistory(t *testing.T) {
	codec, err := NewAESGCMCodec("k2", map[string][]byte{"k2": replayTestKey})
	if err != nil {
		t.Fatal(err)
	}
	replayer, err := worker.NewWorkflowReplayerWithOptions(worker.WorkflowReplayerOptions{
		DataConverter: NewDataConverter(codec),
	})
	if err != nil {
		t.Fatal(err)
	}
	replayer.RegisterWorkflow(SyncProviderWorkflow)
	if err := replayer.ReplayWorkflowHistoryFromJSONFile(nil, "testdata/sync_provider_history.json"); err != nil {
		t.Fatalf("replay: %v", err)
	}
}
