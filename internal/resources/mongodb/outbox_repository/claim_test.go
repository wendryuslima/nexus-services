package outboxrepository

import (
	"go.mongodb.org/mongo-driver/v2/bson"
	"testing"
)

func TestClaimPayloadPreservesOptionalClientMessageID(t *testing.T) {
	for _, key := range []string{"", "018f92d4-2c47-7c87-b33e-d2ea331d2ff2"} {
		payload := bson.M{"messageId": "message-1"}
		if key != "" {
			payload["clientMessageId"] = key
		}
		raw, err := bson.Marshal(bson.M{"_id": "event-1", "payload": payload})
		if err != nil {
			t.Fatal(err)
		}
		var document claimedEventDocument
		if err := bson.Unmarshal(raw, &document); err != nil {
			t.Fatal(err)
		}
		result := document.toPort()
		if result.Payload.ClientMessageID != key || result.ID != "event-1" {
			t.Fatalf("mapping lost payload: %+v", result)
		}
	}
}
