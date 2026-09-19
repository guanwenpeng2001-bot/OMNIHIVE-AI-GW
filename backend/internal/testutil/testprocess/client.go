// Package testprocess runs independent copies of a test binary with a JSON pipe.
package testprocess

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

const roleVariable = "SUB2API_INDEPENDENT_TEST_ROLE"
const messagePrefix = "INDEPENDENT_CLIENT_JSON:"

func Role() string { return os.Getenv(roleVariable) }

// Serve exits on stdin EOF. A failed assertion still fails the child test.
func Serve(test *testing.T, handle func(json.RawMessage) any) {
	test.Helper()
	decoder := json.NewDecoder(os.Stdin)
	for {
		var request json.RawMessage
		err := decoder.Decode(&request)
		if err == io.EOF {
			return
		}
		require.NoError(test, err)
		response, err := json.Marshal(handle(request))
		require.NoError(test, err)
		_, err = os.Stdout.WriteString(messagePrefix + string(response) + "\n")
		require.NoError(test, err)
	}
}

type Client struct {
	encoder   *json.Encoder
	responses chan []byte
	output    bytes.Buffer
	cancel    context.CancelFunc
	test      *testing.T
}

func Start(test *testing.T, role string) *Client {
	test.Helper()
	executable, err := os.Executable()
	require.NoError(test, err)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	command := exec.CommandContext(ctx, executable, "-test.run=^"+regexp.QuoteMeta(strings.Split(test.Name(), "/")[0])+"$", "-test.v")
	command.Env = append(os.Environ(), roleVariable+"="+role)
	input, err := command.StdinPipe()
	require.NoError(test, err)
	output, err := command.StdoutPipe()
	require.NoError(test, err)
	client := &Client{encoder: json.NewEncoder(input), responses: make(chan []byte, 16), cancel: cancel, test: test}
	command.Stderr = &client.output
	require.NoError(test, command.Start())
	var transcript bytes.Buffer
	readerDone := make(chan struct{})
	go func() {
		defer close(readerDone)
		defer close(client.responses)
		scanner := bufio.NewScanner(output)
		for scanner.Scan() {
			if payload, ok := strings.CutPrefix(scanner.Text(), messagePrefix); ok {
				client.responses <- []byte(payload)
			} else {
				_, _ = transcript.WriteString(scanner.Text() + "\n")
			}
		}
	}()
	test.Cleanup(func() {
		_ = input.Close()
		err := command.Wait()
		cancel()
		<-readerDone
		test.Logf("participant=%s pid=%d exit=%v stderr=%s", role, command.Process.Pid, err, client.output.String())
		if err != nil {
			test.Logf("child transcript: %s", transcript.String())
		}
		require.NoError(test, err)
	})
	return client
}

func (client *Client) Send(request any) {
	client.test.Helper()
	require.NoError(client.test, client.encoder.Encode(request))
}

func (client *Client) Receive(response any) {
	client.test.Helper()
	select {
	case payload, open := <-client.responses:
		require.True(client.test, open, "participant exited before response")
		require.NoError(client.test, json.Unmarshal(payload, response))
	case <-time.After(20 * time.Second):
		client.cancel()
		client.test.Fatal("independent participant response timed out")
	}
}
