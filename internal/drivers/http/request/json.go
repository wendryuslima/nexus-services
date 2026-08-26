package request

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"strings"
)

func ValidateJSONContentType(httpRequest *http.Request) error {
	if httpRequest == nil {
		return ErrNilRequest
	}

	contentType := httpRequest.Header.Get("Content-Type")
	mediaType, _, err := mime.ParseMediaType(contentType)
	if err != nil || mediaType != "application/json" {
		return ErrUnsupportedMediaType
	}

	return nil
}

func DecodeJSON(writen http.ResponseWriter, httpRequest *http.Request, destination any, maxyBytes int64) error {
	if httpRequest == nil {
		return ErrNilRequest
	}

	if destination == nil {
		return ErrNilDestination
	}

	if maxyBytes <= 0 {
		return ErrInvalidBodyLimit
	}

	if err := ValidateJSONContentType(httpRequest); err != nil {
		return err
	}

	httpRequest.Body = http.MaxBytesReader(writen, httpRequest.Body, maxyBytes)
	decoder := json.NewDecoder(httpRequest.Body)
	decoder.DisallowUnknownFields()

	if err := decoder.Decode(destination); err != nil {
		return classifyDecodeError(err)
	}
	err := decoder.Decode(&struct{}{})

	switch {
	case errors.Is(err, io.EOF):
		return nil

	case err == nil:
		return ErrMultipleJSONValues

	default:
		return classifyTrailingJSONError(err)
	}
}

func classifyDecodeError(err error) error {
	if errors.Is(err, io.EOF) {
		return ErrEmptyBody
	}

	var syntaxError *json.SyntaxError
	if errors.As(err, &syntaxError) {
		return fmt.Errorf(
			"%w: syntax error near byte %d",
			ErrMalformedJSON,
			syntaxError.Offset,
		)
	}

	var typeError *json.UnmarshalTypeError
	if errors.As(err, &typeError) {
		return fmt.Errorf("%w: invalid value for field %s",
			ErrMalformedJSON,
			typeError.Field)
	}

	if strings.HasPrefix(err.Error(), "json: unknown field") {
		return fmt.Errorf("%w: unknown field",
			ErrMalformedJSON)
	}
	return fmt.Errorf("decode JSON request: %w",
		err)
}

func classifyTrailingJSONError(err error) error {
	var maxBytesError *http.MaxBytesError
	if errors.As(err, &maxBytesError) {
		return ErrBodyTooLarge
	}

	return fmt.Errorf("%w: invalid trailing content",
		ErrMalformedJSON)
}
