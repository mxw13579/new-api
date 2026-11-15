package openai

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/constant"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOpenaiRealtimeHandlerRejectsOversizedClientMessage(t *testing.T) {
	previousLimit := constant.MaxRequestBodyMB
	constant.MaxRequestBodyMB = 1
	t.Cleanup(func() { constant.MaxRequestBodyMB = previousLimit })

	serverConns := make(chan *websocket.Conn, 2)
	upgrader := websocket.Upgrader{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		serverConns <- conn
	}))
	t.Cleanup(server.Close)
	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")

	// user <-> relayClient is the downstream leg; relayTarget <-> upstream is the upstream leg.
	user, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = user.Close() })
	relayClient := <-serverConns
	t.Cleanup(func() { _ = relayClient.Close() })
	relayTarget, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = relayTarget.Close() })
	upstream := <-serverConns
	t.Cleanup(func() { _ = upstream.Close() })

	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodGet, "/v1/realtime", nil)
	info := &relaycommon.RelayInfo{ClientWs: relayClient, TargetWs: relayTarget}

	done := make(chan struct{})
	go func() {
		defer close(done)
		OpenaiRealtimeHandler(c, info)
	}()

	// The write may block once the relay stops reading; closing the
	// connection during cleanup releases it.
	go func() {
		_ = user.WriteMessage(websocket.TextMessage, bytes.Repeat([]byte("a"), 1<<20+1))
	}()

	require.NoError(t, user.SetReadDeadline(time.Now().Add(5*time.Second)))
	_, _, err = user.ReadMessage()
	assert.True(t, websocket.IsCloseError(err, websocket.CloseMessageTooBig), "unexpected read result: %v", err)

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("realtime handler did not return after oversized client message")
	}
}
