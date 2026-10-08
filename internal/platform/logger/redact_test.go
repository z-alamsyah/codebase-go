package logger

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/structpb"
)

func TestRedactor_JSON(t *testing.T) {
	r := NewRedactor([]string{"password", "token", "Authorization"}, 0)

	tests := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "top level key",
			in:   `{"email":"a@b.c","password":"secret"}`,
			want: `{"email":"a@b.c","password":"[REDACTED]"}`,
		},
		{
			name: "key matching is case-insensitive",
			in:   `{"PassWord":"secret","AUTHORIZATION":"Bearer x"}`,
			want: `{"AUTHORIZATION":"[REDACTED]","PassWord":"[REDACTED]"}`,
		},
		{
			name: "nested objects and arrays",
			in:   `{"user":{"name":"a","token":"t"},"items":[{"password":"p"},{"ok":1}]}`,
			want: `{"items":[{"password":"[REDACTED]"},{"ok":1}],"user":{"name":"a","token":"[REDACTED]"}}`,
		},
		{
			name: "non-JSON payload is never logged verbatim",
			in:   `password=secret&email=a`,
			want: `[non-JSON payload omitted, 23 bytes]`,
		},
		{
			name: "empty payload",
			in:   ``,
			want: ``,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, r.JSON([]byte(tt.in)))
		})
	}
}

func TestRedactor_JSON_TruncatesAfterRedaction(t *testing.T) {
	r := NewRedactor([]string{"password"}, 20)
	got := r.JSON([]byte(`{"password":"a-very-long-secret-value","x":"yyyyyyyyyy"}`))
	assert.True(t, strings.HasPrefix(got, `{"password":"[REDACT`), got)
	assert.True(t, strings.HasSuffix(got, "...[truncated]"), got)
	assert.NotContains(t, got, "a-very-long-secret-value")
}

func TestRedactor_Proto(t *testing.T) {
	r := NewRedactor([]string{"password"}, 0)
	msg, err := structpb.NewStruct(map[string]any{"email": "a@b.c", "password": "secret"})
	require.NoError(t, err)

	got := r.Proto(msg)
	assert.Contains(t, got, `"password":"[REDACTED]"`)
	assert.NotContains(t, got, "secret")
}

// The handler must mask every sink: attribute keys, header groups and payloads.
func TestRedactHandler(t *testing.T) {
	var buf bytes.Buffer
	r := NewRedactor([]string{"password", "authorization"}, 0)
	l := slog.New(&redactHandler{next: slog.NewJSONHandler(&buf, nil), r: r})

	l.InfoContext(context.Background(), "request",
		slog.String("password", "plain"),
		slog.Group("http.request.header", slog.String("Authorization", "Bearer abc")),
		slog.String(KeyHTTPRequestBody, `{"name":"a","password":"secret"}`),
	)

	var out map[string]any
	require.NoError(t, json.Unmarshal(buf.Bytes(), &out))
	assert.Equal(t, Redacted, out["password"])
	assert.Equal(t, Redacted, out["http.request.header"].(map[string]any)["Authorization"])
	assert.Equal(t, `{"name":"a","password":"[REDACTED]"}`, out[KeyHTTPRequestBody])
	assert.NotContains(t, buf.String(), "secret")
	assert.NotContains(t, buf.String(), "Bearer abc")
}
