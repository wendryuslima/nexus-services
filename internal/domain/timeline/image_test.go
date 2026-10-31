package timeline

import (
	"strings"
	"testing"
)

func TestImageContentRejectsMixedOrInvalidShapes(t *testing.T) {
	for _, test := range []struct {
		kind               ContentType
		value, id, caption string
		valid              bool
	}{
		{ContentTypeText, "hello", "", "", true},
		{ContentTypeImage, "", "image-1", "", true},
		{ContentTypeImage, "", "image-1", "Olá", true},
		{ContentTypeImage, "hello", "image-1", "", false},
		{ContentTypeText, "hello", "image-1", "", false},
		{ContentTypeText, "hello", "", "caption", false},
		{ContentTypeImage, "", "", "caption", false},
		{ContentTypeImage, "", "image-1", strings.Repeat("🙂", 4001), false},
		{ContentTypeImage, "", "image-1", string([]byte{0xff}), false},
	} {
		content, err := RestoreContent(test.kind, test.value, test.id, test.caption)
		if (err == nil) != test.valid {
			t.Fatalf("shape %+v: %v", test, err)
		}
		if test.valid && (!content.Valid() || content.ImageID() != test.id || content.Caption() != test.caption) {
			t.Fatalf("content lost fields: %+v", content)
		}
	}
}
