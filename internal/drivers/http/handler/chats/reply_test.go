package chats

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	applicationmessages "github.com/wendryuslima/nexus-services/internal/application/messages"
	"github.com/wendryuslima/nexus-services/internal/ports"
)

func TestCreateMessageReplyContract(t *testing.T) {
	for _, unavailable := range []bool{false, true} {
		executor := &createMessageExecutorStub{output: applicationmessages.CreateOutput{
			Created: true, ReplyID: "original", ReplyTo: &ports.MessageReply{SenderID: "user-2", Sequence: 1, Content: ports.OutboxContent{Type: "TEXT", Value: "Original"}},
		}}
		if unavailable {
			executor.err = ports.ErrReplyUnavailable
		}
		handler := newAuthenticatedCreateMessageHandler(t, executor)
		request := httptest.NewRequest(http.MethodPost, "/v1/chats/chat-1/messages", strings.NewReader(`{"clientMessageId":"018f92d4-2c47-7c87-b33e-d2ea331d2ff2","replyID":"original","content":{"type":"TEXT","value":"Resposta"}}`))
		request.Header.Set("Content-Type", "application/json")
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, request)
		if executor.input.ReplyID != "original" {
			t.Fatal("replyID not passed to use case")
		}
		if unavailable {
			if recorder.Code != 422 || !strings.Contains(recorder.Body.String(), `"code":"reply_unavailable"`) {
				t.Fatalf("unexpected error: %s", recorder.Body.String())
			}
			continue
		}
		var response createMessageResponse
		if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
			t.Fatal(err)
		}
		if recorder.Code != 201 || response.Data.ReplyID != "original" || response.Data.ReplyTo == nil || response.Data.ReplyTo.Content.Value != "Original" {
			t.Fatalf("reply missing: %s", recorder.Body.String())
		}
	}
}
