package messages

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/wendryuslima/nexus-services/internal/domain/chat"
	"github.com/wendryuslima/nexus-services/internal/domain/timeline"
	"github.com/wendryuslima/nexus-services/internal/domain/user"
	"github.com/wendryuslima/nexus-services/internal/ports"
)

type createMessageChatRepositoryStub struct {
	conversation *chat.Chat
	err          error
}

func (stub *createMessageChatRepositoryStub) GetOrCreateDirect(
	context.Context,
	*chat.Chat,
) (*chat.Chat, bool, error) {
	panic("not used")
}

func (stub *createMessageChatRepositoryStub) FindByID(
	context.Context,
	chat.ID,
) (*chat.Chat, error) {
	return stub.conversation, stub.err
}

func (stub *createMessageChatRepositoryStub) ListByParticipant(
	context.Context,
	ports.ListChatsParams,
) (ports.ListChatsResult, error) {
	panic("not used")
}

type createMessageStoreStub struct {
	called bool
	params ports.CreateMessageParams
	result ports.CreateMessageResult
	err    error
}

func (stub *createMessageStoreStub) CreateWithOutbox(
	_ context.Context,
	params ports.CreateMessageParams,
) (ports.CreateMessageResult, error) {
	stub.called = true
	stub.params = params
	return stub.result, stub.err
}

type sequentialIDGeneratorStub struct {
	values []string
	index  int
}

func (stub *sequentialIDGeneratorStub) New(context.Context) (string, error) {
	if stub.index >= len(stub.values) {
		return "", errors.New("no more test identifiers")
	}

	value := stub.values[stub.index]
	stub.index++
	return value, nil
}

type fixedClockStub struct {
	now time.Time
}

func (stub fixedClockStub) Now() time.Time {
	return stub.now
}

func TestCreateUseCasePersistsTheMessageAndMapsItsResult(t *testing.T) {
	createdAt := time.Date(2026, 10, 13, 12, 0, 0, 0, time.UTC)
	currentUserID := mustCreateMessageUserID(t, "user-1")
	recipientID := mustCreateMessageUserID(t, "user-2")
	chatID := mustCreateMessageChatID(t, "chat-1")
	conversation, err := chat.NewDirect(chatID, currentUserID, recipientID, createdAt.Add(-time.Hour))
	if err != nil {
		t.Fatalf("create chat: %v", err)
	}

	clientMessageID := "018f92d4-2c47-7c87-b33e-d2ea331d2ff2"
	storedItem := mustCreateMessageItem(
		t,
		"message-1",
		chatID,
		currentUserID,
		clientMessageID,
		"Olá!",
		1,
		createdAt,
	)
	messageStore := &createMessageStoreStub{result: ports.CreateMessageResult{
		Item: storedItem, Created: true,
	}}
	ids := &sequentialIDGeneratorStub{values: []string{"message-1", "event-1"}}
	useCase, err := NewCreateUseCase(CreateDependencies{
		ChatRepository: &createMessageChatRepositoryStub{conversation: conversation},
		MessageStore:   messageStore,
		IDGenerator:    ids,
		Clock:          fixedClockStub{now: createdAt},
	})
	if err != nil {
		t.Fatalf("create use case: %v", err)
	}

	output, err := useCase.Execute(context.Background(), CreateInput{
		CurrentUserID:   currentUserID.String(),
		ChatID:          chatID.String(),
		ClientMessageID: clientMessageID,
		Content: CreateContentInput{
			Type: "TEXT", Value: "Olá!",
		},
	})
	if err != nil {
		t.Fatalf("execute use case: %v", err)
	}

	if !messageStore.called {
		t.Fatal("expected the message store to be called")
	}
	if messageStore.params.MessageID.String() != "message-1" ||
		messageStore.params.EventID != "event-1" ||
		messageStore.params.ChatID.String() != chatID.String() ||
		messageStore.params.Message.SenderID().String() != currentUserID.String() ||
		messageStore.params.Message.ClientMessageID().String() != clientMessageID ||
		!messageStore.params.CreatedAt.Equal(createdAt) {
		t.Fatalf("unexpected message store parameters: %+v", messageStore.params)
	}
	if output.ID != "message-1" || output.Sequence != 1 ||
		output.DeliveryStatus != DeliveryStatusPersisted || !output.Created {
		t.Fatalf("unexpected output: %+v", output)
	}
}

// TestCreateUseCaseDoesNotStoreMessagesForNonParticipants verifies that chat
// membership is checked before IDs are allocated or writes are attempted.
func TestCreateUseCaseDoesNotStoreMessagesForNonParticipants(t *testing.T) {
	now := time.Date(2026, 10, 13, 12, 0, 0, 0, time.UTC)
	firstUserID := mustCreateMessageUserID(t, "user-1")
	secondUserID := mustCreateMessageUserID(t, "user-2")
	outsiderID := mustCreateMessageUserID(t, "user-3")
	chatID := mustCreateMessageChatID(t, "chat-1")
	conversation, err := chat.NewDirect(chatID, firstUserID, secondUserID, now)
	if err != nil {
		t.Fatalf("create chat: %v", err)
	}

	messageStore := &createMessageStoreStub{}
	ids := &sequentialIDGeneratorStub{values: []string{"message-1", "event-1"}}
	useCase, err := NewCreateUseCase(CreateDependencies{
		ChatRepository: &createMessageChatRepositoryStub{conversation: conversation},
		MessageStore:   messageStore,
		IDGenerator:    ids,
		Clock:          fixedClockStub{now: now},
	})
	if err != nil {
		t.Fatalf("create use case: %v", err)
	}

	_, err = useCase.Execute(context.Background(), CreateInput{
		CurrentUserID:   outsiderID.String(),
		ChatID:          chatID.String(),
		ClientMessageID: "018f92d4-2c47-7c87-b33e-d2ea331d2ff2",
		Content:         CreateContentInput{Type: "TEXT", Value: "Olá!"},
	})
	if !errors.Is(err, ErrChatNotFound) {
		t.Fatalf("expected ErrChatNotFound, got %v", err)
	}
	if messageStore.called || ids.index != 0 {
		t.Fatal("a non-participant must not trigger persistence or ID generation")
	}
}

// TestCreateUseCaseMapsIdempotencyConflicts preserves the application-level
// error that the HTTP handler maps to a 409 response.
func TestCreateUseCaseMapsIdempotencyConflicts(t *testing.T) {
	now := time.Date(2026, 10, 13, 12, 0, 0, 0, time.UTC)
	currentUserID := mustCreateMessageUserID(t, "user-1")
	recipientID := mustCreateMessageUserID(t, "user-2")
	chatID := mustCreateMessageChatID(t, "chat-1")
	conversation, err := chat.NewDirect(chatID, currentUserID, recipientID, now)
	if err != nil {
		t.Fatalf("create chat: %v", err)
	}

	useCase, err := NewCreateUseCase(CreateDependencies{
		ChatRepository: &createMessageChatRepositoryStub{conversation: conversation},
		MessageStore:   &createMessageStoreStub{err: ports.ErrMessageIdempotencyConflict},
		IDGenerator:    &sequentialIDGeneratorStub{values: []string{"message-1", "event-1"}},
		Clock:          fixedClockStub{now: now},
	})
	if err != nil {
		t.Fatalf("create use case: %v", err)
	}

	_, err = useCase.Execute(context.Background(), CreateInput{
		CurrentUserID:   currentUserID.String(),
		ChatID:          chatID.String(),
		ClientMessageID: "018f92d4-2c47-7c87-b33e-d2ea331d2ff2",
		Content:         CreateContentInput{Type: "TEXT", Value: "Olá!"},
	})
	if !errors.Is(err, ErrIdempotencyConflict) {
		t.Fatalf("expected ErrIdempotencyConflict, got %v", err)
	}
}

// mustCreateMessageUserID parses stable user identifiers for test setup.
func mustCreateMessageUserID(t *testing.T, value string) user.ID {
	t.Helper()

	id, err := user.ParseID(value)
	if err != nil {
		t.Fatalf("parse user ID: %v", err)
	}

	return id
}

// mustCreateMessageChatID parses stable chat identifiers for test setup.
func mustCreateMessageChatID(t *testing.T, value string) chat.ID {
	t.Helper()

	id, err := chat.ParseID(value)
	if err != nil {
		t.Fatalf("parse chat ID: %v", err)
	}

	return id
}

// mustCreateMessageItem creates the durable item returned by the store stub.
func mustCreateMessageItem(
	t *testing.T,
	itemIDValue string,
	chatID chat.ID,
	senderID user.ID,
	clientMessageIDValue string,
	value string,
	sequenceValue int64,
	createdAt time.Time,
) *timeline.Item {
	t.Helper()

	itemID, err := timeline.ParseID(itemIDValue)
	if err != nil {
		t.Fatalf("parse item ID: %v", err)
	}
	clientMessageID, err := timeline.ParseClientMessageID(clientMessageIDValue)
	if err != nil {
		t.Fatalf("parse client message ID: %v", err)
	}
	sequence, err := timeline.ParseSequence(sequenceValue)
	if err != nil {
		t.Fatalf("parse sequence: %v", err)
	}
	content, err := timeline.NewMessageContent(timeline.ContentTypeText, value)
	if err != nil {
		t.Fatalf("create content: %v", err)
	}
	message, err := timeline.NewMessage(clientMessageID, senderID, content)
	if err != nil {
		t.Fatalf("create message: %v", err)
	}
	item, err := timeline.RestoreMessage(
		itemID,
		chatID,
		sequence,
		createdAt,
		createdAt,
		message,
	)
	if err != nil {
		t.Fatalf("restore item: %v", err)
	}

	return item
}
