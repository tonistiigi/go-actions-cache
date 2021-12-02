package actionscache

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/moby/buildkit/identity"
	"github.com/pkg/errors"
	"github.com/stretchr/testify/require"
)

func init() {
	Log = log.Printf
}

func TestTokenScopes(t *testing.T) {
	// this token is expired
	noValidateToken = true
	defer func() {
		noValidateToken = false
	}()
	c, err := New("eyJ0eXAiOiJKV1QiLCJhbGciOiJSUzI1NiIsIng1dCI6InNiM29QbEhYRi0tV3lEZFBwM0FXRzEtWFhJdyJ9.eyJuYW1laWQiOiJkZGRkZGRkZC1kZGRkLWRkZGQtZGRkZC1kZGRkZGRkZGRkZGQiLCJzY3AiOiJBY3Rpb25zLkdlbmVyaWNSZWFkOjAwMDAwMDAwLTAwMDAtMDAwMC0wMDAwLTAwMDAwMDAwMDAwMCBBY3Rpb25zLlVwbG9hZEFydGlmYWN0czowMDAwMDAwMC0wMDAwLTAwMDAtMDAwMC0wMDAwMDAwMDAwMDAvMTpCdWlsZC9CdWlsZC83NiBMb2NhdGlvblNlcnZpY2UuQ29ubmVjdCBSZWFkQW5kVXBkYXRlQnVpbGRCeVVyaTowMDAwMDAwMC0wMDAwLTAwMDAtMDAwMC0wMDAwMDAwMDAwMDAvMTpCdWlsZC9CdWlsZC83NiIsIklkZW50aXR5VHlwZUNsYWltIjoiU3lzdGVtOlNlcnZpY2VJZGVudGl0eSIsImh0dHA6Ly9zY2hlbWFzLnhtbHNvYXAub3JnL3dzLzIwMDUvMDUvaWRlbnRpdHkvY2xhaW1zL3NpZCI6IkRERERERERELUREREQtRERERC1ERERELURERERERERERERERCIsImh0dHA6Ly9zY2hlbWFzLm1pY3Jvc29mdC5jb20vd3MvMjAwOC8wNi9pZGVudGl0eS9jbGFpbXMvcHJpbWFyeXNpZCI6ImRkZGRkZGRkLWRkZGQtZGRkZC1kZGRkLWRkZGRkZGRkZGRkZCIsImF1aSI6IjVkYzhiNDZmLWQzODctNDYxOS04MTM0LTgyNTAzM2I0NWM1MSIsInNpZCI6ImVjMTY4YTc5LWVmZTctNDc0OC05NjZjLTgwYTdkMjJmNTQ0NyIsImFjIjoiW3tcIlNjb3BlXCI6XCJyZWZzL2hlYWRzL3Rlc3RcIixcIlBlcm1pc3Npb25cIjozfSx7XCJTY29wZVwiOlwicmVmcy9oZWFkcy9tYXN0ZXJcIixcIlBlcm1pc3Npb25cIjoxfV0iLCJvcmNoaWQiOiIyNzEyOTAzZi01NzJjLTQxMjEtYmQwMC1kZDJhOTI0MDczMDIuaGVsbG9fd29ybGRfam9iLl9fZGVmYXVsdCIsImlzcyI6InZzdG9rZW4uYWN0aW9ucy5naXRodWJ1c2VyY29udGVudC5jb20iLCJhdWQiOiJ2c3Rva2VuLmFjdGlvbnMuZ2l0aHVidXNlcmNvbnRlbnQuY29tfHZzbzpmMTE5YzYyNS0yYzU1LTQ1MTgtYThmZC1jZGIyMzliYTNjMGYiLCJuYmYiOjE2MDY4MTI2MjksImV4cCI6MTYwNjgzNTQyOX0.lYlDkfZ6VHimS8Y5NdEmLdIqYwekB3pGBhtg6hLEb3s-Vdm6hLOP-8Ukmi0PWipSaFA33LqC5T-i1OSdM1eRCpcwZ0CES9ii4HcBrsE5JfoyGLHiYiUa5HvRJNDUd9Cbt0w_oghDV7fZ-kMOx7r4mfvaeUQDVS_fs9tCi6LFyG6h6ItYdddTsBfV9yPwjbyBSZIGTXiuaEhEYfJl24P9TRMjPUWYDeA0t_ERohowOlVCnHqJfOfrBtwEipsUN3OujLozYdoiPddhmzmer0D-HLo9VwGQllmyiaEF7MdVi7hjA44phULph62IWiTPbr-1ktOhLMTP1V-8CvF1nse59w", "", Opt{})
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

	c, err := TryEnv(Opt{})
	require.NoError(t, err)
	if c == nil {
		t.SkipNow()
	}

	ce, err := c.Load(ctx, key)
	require.NoError(t, err)
	require.Nil(t, ce)

	err = c.Save(ctx, key, NewBlob([]byte("foobar")))
	require.NoError(t, err)

	ce, err = c.Load(ctx, key)
	require.NoError(t, err)
	require.NotNil(t, ce)

	require.NotEqual(t, ce.Key, "")
	buf := bytes.NewBuffer(nil)
	err = ce.WriteTo(ctx, buf)
	require.NoError(t, err)
	require.Equal(t, "foobar", buf.String())

	ce, err = c.Load(ctx, key[:5])
	require.NoError(t, err)
	require.NotNil(t, ce)

	require.NotEqual(t, ce.Key, "")
	buf = bytes.NewBuffer(nil)
	err = ce.WriteTo(ctx, buf)
	require.NoError(t, err)
	require.Equal(t, "foobar", buf.String())
}

func TestExistingKey(t *testing.T) {
	key := "go-actions-cache-key1"
	ctx := context.TODO()

	c, err := TryEnv(Opt{})
	require.NoError(t, err)
	if c == nil {
		t.SkipNow()
	}

	// may fail because already exists from previous run
	c.Save(ctx, key, NewBlob([]byte("foo1")))

	err = c.Save(ctx, key, NewBlob([]byte("foo2")))
	require.Error(t, err)
	var gae GithubAPIError
	require.True(t, errors.As(err, &gae), "error was %+v", err)
	require.Equal(t, "ArtifactCacheItemAlreadyExistsException", gae.TypeKey)
	require.True(t, errors.Is(err, os.ErrExist))
	var he HTTPError
	require.True(t, errors.As(err, &he), "error was %+v", err)
	require.Equal(t, http.StatusConflict, he.StatusCode)
}

func TestChunkedSave(t *testing.T) {
	ctx := context.TODO()

	c, err := TryEnv(Opt{})
	require.NoError(t, err)
	if c == nil {
		t.SkipNow()
	}
	oldChunkSize := UploadChunkSize
	UploadChunkSize = 2

	id := identity.NewID()
	err = c.Save(ctx, id, NewBlob([]byte("0123456789")))
	require.NoError(t, err)

	UploadChunkSize = oldChunkSize

	ce, err := c.Load(ctx, id)
	require.NoError(t, err)
	require.NotNil(t, ce)

	buf := &bytes.Buffer{}
	err = ce.WriteTo(ctx, buf)
	require.NoError(t, err)

	require.Equal(t, "0123456789", buf.String())

	rac := ce.Download(ctx)
	dt := make([]byte, 3)
	n, err := rac.ReadAt(dt, 2)
	require.NoError(t, err)
	require.Equal(t, 3, n)
	require.Equal(t, "234", string(dt[:n]))

	n, err = rac.ReadAt(dt, 2+3)
	require.NoError(t, err)
	require.Equal(t, 3, n)
	require.Equal(t, "567", string(dt[:n]))

	n, err = rac.ReadAt(dt, 3)
	require.NoError(t, err)
	require.Equal(t, 3, n)
	require.Equal(t, "345", string(dt[:n]))

	n, err = rac.ReadAt(dt, 8)
	require.Error(t, err)
	require.True(t, errors.Is(err, io.EOF))
	require.Equal(t, 2, n)
	require.Equal(t, "89", string(dt[:n]))
}

func TestEncryptedToken(t *testing.T) {
	enc := "U2FsdGVkX18yqVj9dENeyEax1M10IW5sBfxE50BNPe/IrqnC6ZCNxwxaVnE52D4M"
	url, token, err := decryptToken(enc, "bar")
	require.NoError(t, err)
	require.Equal(t, "iamurl", url)
	require.Equal(t, "iamtoken", token)
}

func TestPartialKeyOrder(t *testing.T) {
	ctx := context.TODO()

	c, err := TryEnv(Opt{})
	require.NoError(t, err)
	if c == nil {
		t.SkipNow()
	}

	rand := identity.NewID()

	key1 := "partial-" + rand + "foo22"
	dt := []byte("foo2")
	err = c.Save(ctx, key1, NewBlob(dt))
	require.NoError(t, err)

	key2 := "partial-" + rand + "fo"
	dt = []byte("fo")
	err = c.Save(ctx, key2, NewBlob(dt))
	require.NoError(t, err)

	key3 := "partial-" + rand + "foo1"
	dt = []byte("foo2")
	err = c.Save(ctx, key3, NewBlob(dt))
	require.NoError(t, err)

	ce, err := c.Load(ctx, "partial-"+rand+"foo")
	require.NoError(t, err)
	require.Equal(t, "partial-"+rand+"foo1", ce.Key)

	ce, err = c.Load(ctx, "partial-"+rand)
	require.NoError(t, err)
	require.Equal(t, "partial-"+rand+"foo1", ce.Key)

	ce, err = c.Load(ctx, "partial-"+rand+"foo3")
	require.NoError(t, err)
	require.Nil(t, ce)

	ce, err = c.Load(ctx, "partial-"+rand+"foo2")
	require.NoError(t, err)
	require.Equal(t, "partial-"+rand+"foo22", ce.Key)
}

func TestMutable(t *testing.T) {
	ctx := context.TODO()

	c, err := TryEnv(Opt{})
	require.NoError(t, err)
	if c == nil {
		t.SkipNow()
	}

	key := "mutable-" + identity.NewID()

	err = c.SaveMutable(ctx, key, 10*time.Second, func(ce *Entry) (Blob, error) {
		require.Nil(t, ce)
		return NewBlob([]byte("abc")), nil
	})
	require.NoError(t, err)

	err = c.SaveMutable(ctx, key, 10*time.Second, func(ce *Entry) (Blob, error) {
		require.NotNil(t, ce)
		require.Equal(t, fmt.Sprintf("%s#%d", key, 1), ce.Key)
		buf := &bytes.Buffer{}
		err := ce.WriteTo(ctx, buf)
		require.NoError(t, err)
		return NewBlob(append(buf.Bytes(), []byte("def")...)), nil
	})
	require.NoError(t, err)

	ce, err := c.Load(ctx, key)
	require.NoError(t, err)
	require.NotNil(t, ce)

	buf := &bytes.Buffer{}
	err = ce.WriteTo(ctx, buf)
	require.NoError(t, err)

	require.Equal(t, "abcdef", buf.String())
}

func TestMutableRace(t *testing.T) {
	ctx := context.TODO()

	c, err := TryEnv(Opt{})
	require.NoError(t, err)
	if c == nil {
		t.SkipNow()
	}

	key := "mutable-race-" + identity.NewID()

	err = c.SaveMutable(ctx, key, 10*time.Second, func(ce *Entry) (Blob, error) {
		require.Nil(t, ce)
		return NewBlob([]byte("123")), nil
	})
	require.NoError(t, err)

	addAnother := func() {
		err = c.SaveMutable(ctx, key, 10*time.Second, func(ce *Entry) (Blob, error) {
			require.NotNil(t, ce)
			buf := &bytes.Buffer{}
			err := ce.WriteTo(ctx, buf)
			require.NoError(t, err)
			return NewBlob(append(buf.Bytes(), []byte("456")...)), nil
		})
		require.NoError(t, err)
	}

	count := 0
	err = c.SaveMutable(ctx, key, 10*time.Second, func(ce *Entry) (Blob, error) {
		require.NotNil(t, ce)
		buf := &bytes.Buffer{}
		err := ce.WriteTo(ctx, buf)
		require.NoError(t, err)
		if count == 0 {
			require.Equal(t, fmt.Sprintf("%s#%d", key, 1), ce.Key)
			addAnother()
		} else {
			require.NotEqual(t, fmt.Sprintf("%s#%d", key, 1), ce.Key)
		}
		count++
		return NewBlob(append(buf.Bytes(), []byte("789")...)), nil
	})
	require.NoError(t, err)

	ce, err := c.Load(ctx, key)
	require.NoError(t, err)
	require.NotNil(t, ce)

	buf := &bytes.Buffer{}
	err = ce.WriteTo(ctx, buf)
	require.NoError(t, err)

	require.Equal(t, "123456789", buf.String())
}

func TestMutableCrash(t *testing.T) {
	ctx := context.TODO()

	c, err := TryEnv(Opt{})
	require.NoError(t, err)
	if c == nil {
		t.SkipNow()
	}

	key := "mutable-race-" + identity.NewID()

	// reserve key but don't do anything, as if crashed
	_, err = c.reserve(ctx, fmt.Sprintf("%s#%d", key, 1))
	require.NoError(t, err)

	count := 0
	err = c.SaveMutable(ctx, key, 3*time.Second, func(ce *Entry) (Blob, error) {
		require.Nil(t, ce)
		count++
		return NewBlob([]byte("123")), nil
	})
	require.NoError(t, err)

	require.True(t, count > 1)

	ce, err := c.Load(ctx, key)
	require.NoError(t, err)
	require.NotNil(t, ce)

	require.Equal(t, fmt.Sprintf("%s#%d", key, 2), ce.Key)
}
