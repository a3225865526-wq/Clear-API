package main

import (
	"bufio"
	"io"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHTTPServerReclaimsSlowBodyWithoutCuttingStream(t *testing.T) {
	finish := make(chan struct{})
	defer close(finish)
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/stream" {
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = io.WriteString(w, "data: first\n\n")
			w.(http.Flusher).Flush()
			<-finish
			_, _ = io.WriteString(w, "data: last\n\n")
			return
		}
		if _, err := io.ReadAll(r.Body); err != nil {
			w.WriteHeader(http.StatusRequestTimeout)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
	server := newHTTPServer("127.0.0.1:0", handler)
	require.Positive(t, server.ReadTimeout)
	require.Positive(t, server.IdleTimeout)
	require.Zero(t, server.WriteTimeout)
	server.ReadTimeout = 50 * time.Millisecond
	listener, err := net.Listen("tcp", server.Addr)
	require.NoError(t, err)
	defer server.Close()
	go func() { _ = server.Serve(listener) }()
	client := &http.Client{Timeout: 3 * time.Second}
	url := "http://" + listener.Addr().String()
	response, err := client.Get(url + "/ok")
	require.NoError(t, err)
	assert.Equal(t, 204, response.StatusCode)
	response.Body.Close()
	stream, err := client.Get(url + "/stream")
	require.NoError(t, err)
	defer stream.Body.Close()
	reader := bufio.NewReader(stream.Body)
	line, err := reader.ReadString('\n')
	require.NoError(t, err)
	assert.Equal(t, "data: first\n", line)
	connection, err := net.Dial("tcp", listener.Addr().String())
	require.NoError(t, err)
	defer connection.Close()
	require.NoError(t, connection.SetDeadline(time.Now().Add(3*time.Second)))
	_, err = io.WriteString(connection, "POST /slow HTTP/1.1\r\nHost: localhost\r\nContent-Length: 100\r\n\r\nx")
	require.NoError(t, err)
	slow, err := http.ReadResponse(bufio.NewReader(connection), nil)
	require.NoError(t, err)
	assert.Equal(t, http.StatusRequestTimeout, slow.StatusCode)
	slow.Body.Close()
	finish <- struct{}{}
	remaining, err := io.ReadAll(reader)
	require.NoError(t, err)
	assert.Contains(t, string(remaining), "data: last")
}
