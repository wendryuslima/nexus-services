package timeline

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/wendryuslima/nexus-services/internal/domain/chat"
	domaintimeline "github.com/wendryuslima/nexus-services/internal/domain/timeline"
	"github.com/wendryuslima/nexus-services/internal/domain/user"
	"github.com/wendryuslima/nexus-services/internal/ports"
)

type chatRepositoryStub struct {
	conversation *chat.Chat
	err          error
}

func (stub *chatRepositoryStub) FindByID(context.Context, chat.ID) (*chat.Chat, error) {
	return stub.conversation, stub.err
}

func (stub *chatRepositoryStub) GetOrCreateDirect(context.Context, *chat.Chat) (*chat.Chat, bool, error) {
	panic("not used")
}

func (stub *chatRepositoryStub) ListByParticipant(context.Context, ports.ListChatsParams) (ports.ListChatsResult, error) {
	panic("not used")
}

type timelineRepositoryStub struct {
	result ports.ListTimelineResult
	called bool
	params ports.ListTimelineParams
}

func (stub *timelineRepositoryStub) ListByChat(_ context.Context, params ports.ListTimelineParams) (ports.ListTimelineResult, error) {
	stub.called = true
	stub.params = params
	return stub.result, nil
}

func TestListUseCaseCalculatesIsMineForCurrentUser(t *testing.T) {
	now := time.Date(2026, 9, 27, 0, 3, 46, 0, time.UTC)
	currentUserID := mustUserID(t, "user-1")
	otherUserID := mustUserID(t, "user-2")
	chatID := mustChatID(t, "chat-1")
	conversation, err := chat.NewDirect(chatID, currentUserID, otherUserID, now.Add(-time.Hour))
	if err != nil {
		t.Fatalf("create chat: %v", err)
	}
	first := mustMessageItem(
		t,
		"item-1",
		"018f92d4-2c47-7c87-b33e-d2ea331d2ff1",
		1,
		chatID,
		otherUserID,
		"Bom dia!",
		now,
	)

	second := mustMessageItem(
		t,
		"item-2",
		"018f92d4-2c47-7c87-b33e-d2ea331d2ff2",
		2,
		chatID,
		currentUserID,
		"Estou bem.",
		now.Add(time.Minute),
	)
	nextItemID, _ := domaintimeline.ParseID("item-1")
	timelineRepository := &timelineRepositoryStub{result: ports.ListTimelineResult{
		Items: []*domaintimeline.Item{first, second}, HasNext: true,
		NextCursor: &ports.TimelineCursor{ItemID: nextItemID, CreatedAt: now},
	}}
	useCase, err := NewListUseCase(ListDependencies{
		ChatRepository: &chatRepositoryStub{conversation: conversation}, TimelineRepository: timelineRepository,
	})
	if err != nil {
		t.Fatalf("create use case: %v", err)
	}

	output, err := useCase.Execute(context.Background(), ListInput{
		CurrentUserID: currentUserID.String(), ChatID: chatID.String(),
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if output.Items[0].Message.IsMine {
		t.Error("expected the other participant's message not to be mine")
	}
	if !output.Items[1].Message.IsMine {
		t.Error("expected current user's message to be mine")
	}
	if timelineRepository.params.Limit != defaultPageSize || output.Pagination.PageSize != defaultPageSize {
		t.Fatalf("expected default page size %d", defaultPageSize)
	}
	if output.Pagination.NextCursor == nil || output.Pagination.NextCursor.ID != "item-1" {
		t.Fatal("expected the repository cursor in the output")
	}
}

func TestListUseCaseHidesChatFromNonParticipant(t *testing.T) {
	now := time.Date(2026, 9, 27, 0, 3, 46, 0, time.UTC)
	firstUserID := mustUserID(t, "user-1")
	secondUserID := mustUserID(t, "user-2")
	outsiderID := mustUserID(t, "user-3")
	chatID := mustChatID(t, "chat-1")
	conversation, err := chat.NewDirect(chatID, firstUserID, secondUserID, now)
	if err != nil {
		t.Fatalf("create chat: %v", err)
	}
	timelineRepository := &timelineRepositoryStub{}
	useCase, err := NewListUseCase(ListDependencies{
		ChatRepository: &chatRepositoryStub{conversation: conversation}, TimelineRepository: timelineRepository,
	})
	if err != nil {
		t.Fatalf("create use case: %v", err)
	}

	_, err = useCase.Execute(context.Background(), ListInput{
		CurrentUserID: outsiderID.String(), ChatID: chatID.String(),
	})
	if !errors.Is(err, ErrChatNotFound) {
		t.Fatalf("expected ErrChatNotFound, got %v", err)
	}
	if timelineRepository.called {
		t.Fatal("timeline must not be queried for a non-participant")
	}
}

func mustUserID(t *testing.T, value string) user.ID {
	t.Helper()
	id, err := user.ParseID(value)
	if err != nil {
		t.Fatalf("parse user id: %v", err)
	}
	return id
}

func mustChatID(t *testing.T, value string) chat.ID {
	t.Helper()
	id, err := chat.ParseID(value)
	if err != nil {
		t.Fatalf("parse chat id: %v", err)
	}
	return id
}

func mustMessageItem(
	t *testing.T,
	itemIDValue string,
	clientMessageIDValue string,
	sequenceValue int64,
	chatID chat.ID,
	senderID user.ID,
	value string,
	createdAt time.Time,
) *domaintimeline.Item {
	t.Helper()

	itemID, err := domaintimeline.ParseID(itemIDValue)
	if err != nil {
		t.Fatalf("parse timeline item id: %v", err)
	}

	clientMessageID, err := domaintimeline.ParseClientMessageID(
		clientMessageIDValue,
	)
	if err != nil {
		t.Fatalf("parse client message id: %v", err)
	}

	sequence, err := domaintimeline.ParseSequence(sequenceValue)
	if err != nil {
		t.Fatalf("parse sequence: %v", err)
	}

	content, err := domaintimeline.NewMessageContent(
		domaintimeline.ContentTypeText,
		value,
	)
	if err != nil {
		t.Fatalf("create message content: %v", err)
	}

	message, err := domaintimeline.NewMessage(
		clientMessageID,
		senderID,
		content,
	)
	if err != nil {
		t.Fatalf("create message: %v", err)
	}

	item, err := domaintimeline.RestoreMessage(
		itemID,
		chatID,
		sequence,
		createdAt,
		createdAt,
		message,
	)
	if err != nil {
		t.Fatalf("restore timeline item: %v", err)
	}

	return item
}
