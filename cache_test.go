package actionscache

import (
	"bytes"
	"context"
	"log"
	"testing"

	"github.com/moby/buildkit/identity"
	"github.com/pkg/errors"
	"github.com/stretchr/testify/require"
)

func init() {
	Log = log.Printf
}

func TestTokenScopes(t *testing.T) {
	// this token is expired
	c, err := New("eyJ0eXAiOiJKV1QiLCJhbGciOiJSUzI1NiIsIng1dCI6InNiM29QbEhYRi0tV3lEZFBwM0FXRzEtWFhJdyJ9.eyJuYW1laWQiOiJkZGRkZGRkZC1kZGRkLWRkZGQtZGRkZC1kZGRkZGRkZGRkZGQiLCJzY3AiOiJBY3Rpb25zLkdlbmVyaWNSZWFkOjAwMDAwMDAwLTAwMDAtMDAwMC0wMDAwLTAwMDAwMDAwMDAwMCBBY3Rpb25zLlVwbG9hZEFydGlmYWN0czowMDAwMDAwMC0wMDAwLTAwMDAtMDAwMC0wMDAwMDAwMDAwMDAvMTpCdWlsZC9CdWlsZC83NiBMb2NhdGlvblNlcnZpY2UuQ29ubmVjdCBSZWFkQW5kVXBkYXRlQnVpbGRCeVVyaTowMDAwMDAwMC0wMDAwLTAwMDAtMDAwMC0wMDAwMDAwMDAwMDAvMTpCdWlsZC9CdWlsZC83NiIsIklkZW50aXR5VHlwZUNsYWltIjoiU3lzdGVtOlNlcnZpY2VJZGVudGl0eSIsImh0dHA6Ly9zY2hlbWFzLnhtbHNvYXAub3JnL3dzLzIwMDUvMDUvaWRlbnRpdHkvY2xhaW1zL3NpZCI6IkRERERERERELUREREQtRERERC1ERERELURERERERERERERERCIsImh0dHA6Ly9zY2hlbWFzLm1pY3Jvc29mdC5jb20vd3MvMjAwOC8wNi9pZGVudGl0eS9jbGFpbXMvcHJpbWFyeXNpZCI6ImRkZGRkZGRkLWRkZGQtZGRkZC1kZGRkLWRkZGRkZGRkZGRkZCIsImF1aSI6IjVkYzhiNDZmLWQzODctNDYxOS04MTM0LTgyNTAzM2I0NWM1MSIsInNpZCI6ImVjMTY4YTc5LWVmZTctNDc0OC05NjZjLTgwYTdkMjJmNTQ0NyIsImFjIjoiW3tcIlNjb3BlXCI6XCJyZWZzL2hlYWRzL3Rlc3RcIixcIlBlcm1pc3Npb25cIjozfSx7XCJTY29wZVwiOlwicmVmcy9oZWFkcy9tYXN0ZXJcIixcIlBlcm1pc3Npb25cIjoxfV0iLCJvcmNoaWQiOiIyNzEyOTAzZi01NzJjLTQxMjEtYmQwMC1kZDJhOTI0MDczMDIuaGVsbG9fd29ybGRfam9iLl9fZGVmYXVsdCIsImlzcyI6InZzdG9rZW4uYWN0aW9ucy5naXRodWJ1c2VyY29udGVudC5jb20iLCJhdWQiOiJ2c3Rva2VuLmFjdGlvbnMuZ2l0aHVidXNlcmNvbnRlbnQuY29tfHZzbzpmMTE5YzYyNS0yYzU1LTQ1MTgtYThmZC1jZGIyMzliYTNjMGYiLCJuYmYiOjE2MDY4MTI2MjksImV4cCI6MTYwNjgzNTQyOX0.lYlDkfZ6VHimS8Y5NdEmLdIqYwekB3pGBhtg6hLEb3s-Vdm6hLOP-8Ukmi0PWipSaFA33LqC5T-i1OSdM1eRCpcwZ0CES9ii4HcBrsE5JfoyGLHiYiUa5HvRJNDUd9Cbt0w_oghDV7fZ-kMOx7r4mfvaeUQDVS_fs9tCi6LFyG6h6ItYdddTsBfV9yPwjbyBSZIGTXiuaEhEYfJl24P9TRMjPUWYDeA0t_ERohowOlVCnHqJfOfrBtwEipsUN3OujLozYdoiPddhmzmer0D-HLo9VwGQllmyiaEF7MdVi7hjA44phULph62IWiTPbr-1ktOhLMTP1V-8CvF1nse59w", "")
	require.NoError(t, err)
	require.True(t, len(c.Scopes()) > 0)

	wasWrite := false
	for _, s := range c.Scopes() {
		if s.Permission&PermissionWrite != 0 {
			wasWrite = true
		}
		require.True(t, s.Scope != "")
	}
	require.True(t, wasWrite)
}

func TestSaveLoad(t *testing.T) {
	key := identity.NewID()
	ctx := context.TODO()

	c, err := TryEnv()
	require.NoError(t, err)
	if c == nil {
		t.SkipNow()
	}

	ce, err := c.Load(ctx, key)
	require.NoError(t, err)
	require.Nil(t, ce)

	dt := []byte("foobar")
	err = c.Save(ctx, key, bytes.NewReader(dt), int64(len(dt)))
	require.NoError(t, err)

	ce, err = c.Load(ctx, key)
	require.NoError(t, err)
	require.NotNil(t, ce)

	require.NotEqual(t, ce.Key, "")
	buf := bytes.NewBuffer(nil)
	err = ce.Download(ctx, buf)
	require.NoError(t, err)
	require.Equal(t, "foobar", buf.String())

	ce, err = c.Load(ctx, key[:5])
	require.NoError(t, err)
	require.NotNil(t, ce)

	require.NotEqual(t, ce.Key, "")
	buf = bytes.NewBuffer(nil)
	err = ce.Download(ctx, buf)
	require.NoError(t, err)
	require.Equal(t, "foobar", buf.String())
}

func TestExistingKey(t *testing.T) {
	key := "go-actions-cache-key1"
	ctx := context.TODO()

	c, err := TryEnv()
	require.NoError(t, err)
	if c == nil {
		t.SkipNow()
	}

	dt := []byte("foo1")
	// may fail because already exists from previous run
	c.Save(ctx, key, bytes.NewReader(dt), int64(len(dt)))

	dt = []byte("foo2")
	err = c.Save(ctx, key, bytes.NewReader(dt), int64(len(dt)))
	require.Error(t, err)
	var gae GithubAPIError
	require.True(t, errors.As(err, &gae))
	require.Equal(t, "ArtifactCacheItemAlreadyExistsException", gae.TypeKey)
}
