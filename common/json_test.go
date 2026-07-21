package common

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestJsonRawMessageToString(t *testing.T) {
	tests := []struct {
		name string
		data json.RawMessage
		want string
	}{
		{
			name: "object",
			data: json.RawMessage(`{"city":"Paris","days":0,"strict":false}`),
			want: `{"city":"Paris","days":0,"strict":false}`,
		},
		{
			name: "string",
			data: json.RawMessage(`"{\"city\":\"Paris\",\"days\":0,\"strict\":false}"`),
			want: `{"city":"Paris","days":0,"strict":false}`,
		},
		{
			name: "null",
			data: json.RawMessage(`null`),
			want: "",
		},
		{
			name: "empty",
			data: nil,
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, JsonRawMessageToString(tt.data))
		})
	}
}

type strictJSONObjectFixture struct {
	Name   string `json:"name"`
	Nested struct {
		Value string `json:"value"`
	} `json:"nested"`
}

func TestDecodeStrictJSONObjectDuplicateNested(t *testing.T) {
	var target strictJSONObjectFixture
	err := DecodeStrictJSONObject(
		strings.NewReader(`{"name":"invoice","nested":{"value":"first","value":"second"}}`),
		&target,
		false,
	)
	require.Error(t, err)
}

func TestDecodeStrictJSONObjectUnknownField(t *testing.T) {
	var target strictJSONObjectFixture
	err := DecodeStrictJSONObject(
		strings.NewReader(`{"name":"invoice","nested":{"value":"ok","unknown":true}}`),
		&target,
		false,
	)
	require.Error(t, err)
}

func TestDecodeStrictJSONObjectTrailingValue(t *testing.T) {
	tests := []string{
		`{"name":"invoice","nested":{"value":"ok"}} {}`,
		`{"name":"invoice","nested":{"value":"ok"}} trailing`,
	}
	for _, input := range tests {
		var target strictJSONObjectFixture
		err := DecodeStrictJSONObject(strings.NewReader(input), &target, false)
		require.Error(t, err, input)
	}
}

func TestDecodeStrictJSONObjectZeroBytePolicy(t *testing.T) {
	t.Run("zero bytes rejected by default", func(t *testing.T) {
		var target struct{}
		require.Error(t, DecodeStrictJSONObject(strings.NewReader(""), &target, false))
	})

	t.Run("zero bytes explicitly allowed", func(t *testing.T) {
		var target struct{}
		require.NoError(t, DecodeStrictJSONObject(strings.NewReader(""), &target, true))
	})

	t.Run("whitespace is not zero bytes", func(t *testing.T) {
		var target struct{}
		require.Error(t, DecodeStrictJSONObject(strings.NewReader(" \r\n\t"), &target, true))
	})

	t.Run("empty object is allowed", func(t *testing.T) {
		var target struct{}
		require.NoError(t, DecodeStrictJSONObject(strings.NewReader("{}"), &target, true))
	})

	t.Run("top level must be an object", func(t *testing.T) {
		for _, input := range []string{"null", `[]`, `"value"`, "1", "true"} {
			var target struct{}
			require.Error(t, DecodeStrictJSONObject(strings.NewReader(input), &target, true), input)
		}
	})

	t.Run("target must be a nonnil struct pointer", func(t *testing.T) {
		var nilTarget *struct{}
		require.Error(t, DecodeStrictJSONObject(strings.NewReader("{}"), nilTarget, false))
		require.Error(t, DecodeStrictJSONObject(strings.NewReader("{}"), struct{}{}, false))
		var mapTarget map[string]any
		require.Error(t, DecodeStrictJSONObject(strings.NewReader("{}"), &mapTarget, false))
	})

	t.Run("valid object decodes", func(t *testing.T) {
		var target strictJSONObjectFixture
		require.NoError(t, DecodeStrictJSONObject(
			strings.NewReader(`{"name":"invoice","nested":{"value":"ok"}}`),
			&target,
			false,
		))
		assert.Equal(t, "invoice", target.Name)
		assert.Equal(t, "ok", target.Nested.Value)
	})
}
