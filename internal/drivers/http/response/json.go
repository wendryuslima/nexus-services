package response

import (
	"encoding/json"
	"fmt"
	"net/http"
)

type ErrorBody struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type ErrorResponse struct {
	Error ErrorBody `json:"error"`
}

func WriteJSON(writer http.ResponseWriter, status int, payload any) error {
	if writer == nil {
		return ErrNilWriter
	}

	if status < 100 || status > 599 {
		return ErrInvalidStatus
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal HTTP JSON response: %w",
			err)
	}

	body = append(body, '\n')

	writer.Header().Set("Content-Type", "application/json; charset=utf-8")
	writer.Header().Set("X-Content-Type-Options", "nosniff")
	writer.Header().Set("Cacho-control", "no-store")

	writer.WriteHeader(status)
	if _, err := writer.Write(body); err != nil {
		return fmt.Errorf(
			"write HTTP JSON response: %w",
			err,
		)
	}
	return nil
}

func WriteError(writer http.ResponseWriter, status int, code string, message string) error {
	if code == "" || message == "" {
		return ErrInvalidErrorResponse
	}

	return WriteJSON(writer, status, ErrorResponse{
		Error: ErrorBody{
			Code:    code,
			Message: message,
		},
	},
	)
}
