package main

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/png"
	"io"
	"mime/multipart"
	"net/http"
	"testing"
	"time"

	coder "github.com/coder/websocket"
	"github.com/nats-io/nats.go/jetstream"
	applicationimages "github.com/wendryuslima/nexus-services/internal/application/images"
	applicationoutbox "github.com/wendryuslima/nexus-services/internal/application/outbox"
	"github.com/wendryuslima/nexus-services/internal/ports"
	"github.com/wendryuslima/nexus-services/internal/resources/config"
	"github.com/wendryuslima/nexus-services/internal/resources/garage"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
)

type e2eOutboxStep interface {
	RunOnce(context.Context) (applicationoutbox.Outcome, error)
}

func e2eImageFlow(t *testing.T, ctx context.Context, base string, alice, bob, eve *http.Client, chatID string, step e2eOutboxStep, consumer jetstream.Consumer, dispatcher ports.MessageDeliveryHandler, socket *coder.Conn, db *mongo.Database) {
	t.Helper()
	var file bytes.Buffer
	_ = png.Encode(&file, image.NewNRGBA(image.Rect(0, 0, 3, 2)))
	clientUploadID := "00000000-0000-4000-8000-000000000001"
	upload := func(data []byte, expected int) applicationimages.Metadata {
		t.Helper()
		var body bytes.Buffer
		writer := multipart.NewWriter(&body)
		_ = writer.WriteField("chatId", chatID)
		_ = writer.WriteField("clientUploadId", clientUploadID)
		part, _ := writer.CreateFormFile("file", "image.png")
		_, _ = part.Write(data)
		_ = writer.Close()
		request, _ := http.NewRequestWithContext(ctx, "POST", base+"/v1/images", &body)
		request.Header.Set("Content-Type", writer.FormDataContentType())
		request.Header.Set("Origin", "http://localhost:5172")
		response, err := alice.Do(request)
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		raw, _ := io.ReadAll(response.Body)
		if response.StatusCode != expected {
			t.Fatalf("image upload: HTTP %d: %s", response.StatusCode, raw)
		}
		var result struct {
			Data applicationimages.Metadata `json:"data"`
		}
		if expected == 200 {
			if err := json.Unmarshal(raw, &result); err != nil {
				t.Fatal(err)
			}
		}
		return result.Data
	}
	asset := upload(file.Bytes(), 200)
	if asset.ID == "" || asset.Width != 3 || asset.Height != 2 || asset.ContentType != "image/png" {
		t.Fatalf("invalid metadata: %+v", asset)
	}
	if again := upload(file.Bytes(), 200); again.ID != asset.ID {
		t.Fatal("retry duplicated image")
	}
	var changed bytes.Buffer
	_ = png.Encode(&changed, image.NewNRGBA(image.Rect(0, 0, 4, 2)))
	upload(changed.Bytes(), 409)
	cfg, err := config.LoadImagesConfig()
	if err != nil {
		t.Fatal(err)
	}
	storage, err := garage.New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	var location struct {
		Bucket    string `bson:"bucket"`
		ObjectKey string `bson:"object_key"`
	}
	if err := db.Collection("images").FindOne(ctx, bson.M{"_id": asset.ID}).Decode(&location); err != nil {
		t.Fatal(err)
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = storage.Delete(cleanup, location.Bucket, location.ObjectKey)
	}()
	assertStatus := func(client *http.Client, method, target string, payload any, expected int) {
		t.Helper()
		var body io.Reader
		if payload != nil {
			raw, _ := json.Marshal(payload)
			body = bytes.NewReader(raw)
		}
		request, _ := http.NewRequestWithContext(ctx, method, target, body)
		request.Header.Set("Origin", "http://localhost:5172")
		if payload != nil {
			request.Header.Set("Content-Type", "application/json")
		}
		response, err := client.Do(request)
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		raw, _ := io.ReadAll(response.Body)
		if response.StatusCode != expected {
			t.Fatalf("%s: HTTP %d: %s", request.URL.Path, response.StatusCode, raw)
		}
	}
	accessPath := base + "/v1/images/" + asset.ID + "/access"
	assertStatus(bob, "GET", accessPath, nil, 404)
	content := map[string]string{"type": "IMAGE", "imageId": asset.ID, "caption": "Uma imagem 🙂"}
	body := map[string]any{"clientMessageId": "00000000-0000-4000-8000-000000000002", "content": content}
	messagePath := base + "/v1/chats/" + chatID + "/messages"
	assertStatus(bob, "POST", messagePath, body, 409)
	e2eRequest(t, alice, "POST", messagePath, body)
	if again := upload(file.Bytes(), 200); again.ID != asset.ID {
		t.Fatal("attached upload retry duplicated image")
	}
	e2eRequest(t, alice, "POST", messagePath, body)
	if outcome, err := step.RunOnce(ctx); err != nil || outcome != applicationoutbox.OutcomePublished {
		t.Fatalf("publish image: %s %v", outcome, err)
	}
	queued, err := consumer.Next(jetstream.FetchMaxWait(3 * time.Second))
	if err != nil {
		t.Fatal(err)
	}
	var internal ports.MessageCreatedEvent
	if err := json.Unmarshal(queued.Data(), &internal); err != nil {
		t.Fatal(err)
	}
	if internal.Payload.Content.ImageID != asset.ID || internal.Payload.Content.Caption != "Uma imagem 🙂" {
		t.Fatalf("outbox lost image content: %+v", internal)
	}
	if err := dispatcher.Dispatch(ctx, internal); err != nil {
		t.Fatal(err)
	}
	if err := queued.DoubleAck(ctx); err != nil {
		t.Fatal(err)
	}
	readCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	_, raw, err := socket.Read(readCtx)
	if err != nil {
		t.Fatal(err)
	}
	var event ports.RealtimeMessageCreated
	if err := json.Unmarshal(raw, &event); err != nil {
		t.Fatal(err)
	}
	if event.Data.Content.Type != "IMAGE" || event.Data.Content.ImageID != asset.ID || event.Data.Content.Caption != "Uma imagem 🙂" {
		t.Fatalf("realtime lost image: %+v", event)
	}
	assertStatus(eve, "GET", accessPath, nil, 404)
	accessRaw := e2eRequest(t, bob, "GET", accessPath, nil)
	var access struct {
		Data applicationimages.Access `json:"data"`
	}
	if err := json.Unmarshal(accessRaw, &access); err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(accessRaw, []byte("objectKey")) || bytes.Contains(accessRaw, []byte("secret")) {
		t.Fatal("private coordinates leaked in DTO")
	}
	if time.Until(access.Data.ExpiresAt) < 9*time.Minute {
		t.Fatal("incorrect signed URL lifetime")
	}
	response, err := http.Get(access.Data.URL)
	if err != nil {
		t.Fatal(err)
	}
	downloaded, _ := io.ReadAll(response.Body)
	response.Body.Close()
	if response.StatusCode != 200 || !bytes.Equal(downloaded, file.Bytes()) {
		t.Fatalf("signed download: status %d", response.StatusCode)
	}
	timelineRaw := e2eRequest(t, bob, "GET", base+"/v1/chats/"+chatID+"/timeline?afterSequence=0", nil)
	var timelineResponse struct {
		Data struct {
			Items []struct {
				Message struct {
					Content ports.OutboxContent `json:"content"`
				} `json:"message"`
			} `json:"items"`
		} `json:"data"`
	}
	if err := json.Unmarshal(timelineRaw, &timelineResponse); err != nil {
		t.Fatal(err)
	}
	if len(timelineResponse.Data.Items) != 2 || timelineResponse.Data.Items[1].Message.Content.ImageID != asset.ID {
		t.Fatal("timeline did not recover image content")
	}
	if count, err := db.Collection("images").CountDocuments(ctx, bson.M{}); err != nil || count != 1 {
		t.Fatalf("duplicate image metadata: %d %v", count, err)
	}
}
