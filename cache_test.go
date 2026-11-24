package actionscache

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"testing/synctest"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/pkg/errors"
	"github.com/stretchr/testify/require"
)

func init() {
	Log = log.Printf
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestRetryResponses(t *testing.T) {
	for _, tc := range []struct {
		status   int
		attempts int
	}{
		{
			status:   http.StatusTooManyRequests,
			attempts: 2,
		},
		{
			status:   http.StatusBadGateway,
			attempts: 2,
		},
		{
			status:   http.StatusServiceUnavailable,
			attempts: 2,
		},
		{
			status:   http.StatusGatewayTimeout,
			attempts: 2,
		},
		{
			status:   http.StatusForbidden,
			attempts: 1,
		},
		{
			status:   http.StatusNotImplemented,
			attempts: 1,
		},
	} {
		t.Run(http.StatusText(tc.status), func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				attempts := 0
				c := &Cache{opt: Opt{
					Timeout:     time.Minute,
					BackoffPool: &BackoffPool{},
					Client: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
						attempts++
						body, err := io.ReadAll(req.Body)
						require.NoError(t, err)
						require.NoError(t, req.Body.Close())
						require.Equal(t, "payload", string(body))
						status := tc.status
						if attempts > 1 {
							status = http.StatusOK
						}
						return &http.Response{
							StatusCode: status,
							Body:       io.NopCloser(bytes.NewBufferString("upstream unavailable")),
						}, nil
					})},
				}}
				start := time.Now()
				resp, err := c.doWithRetries(context.Background(), &request{
					method: http.MethodPost,
					url:    "https://cache.example/test",
					body:   func() io.Reader { return bytes.NewBufferString("payload") },
				})
				if tc.attempts > 1 {
					require.NoError(t, err)
					require.NoError(t, resp.Body.Close())
					require.GreaterOrEqual(t, time.Since(start), minBackoff/2)
					require.Less(t, time.Since(start), minBackoff)
				} else {
					require.Error(t, err)
				}
				require.Equal(t, tc.attempts, attempts)
			})
		})
	}
}

func TestRetryResponseTimeout(t *testing.T) {
	for _, retryAfter := range []string{"", "0", "2"} {
		t.Run("RetryAfter="+retryAfter, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				pool := &BackoffPool{}
				defer pool.Reset()
				c := &Cache{opt: Opt{
					Timeout:     3 * time.Second,
					BackoffPool: pool,
					Client: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
						time.Sleep(100 * time.Millisecond)
						return &http.Response{
							StatusCode: http.StatusServiceUnavailable,
							Header:     http.Header{"Retry-After": []string{retryAfter}},
							Body:       io.NopCloser(bytes.NewBufferString("no healthy upstream")),
						}, nil
					})},
				}}
				start := time.Now()
				_, err := c.doWithRetries(context.Background(), &request{
					method: http.MethodGet,
					url:    "https://cache.example/test",
				})
				require.ErrorContains(t, err, "maximum timeout")
				require.ErrorContains(t, err, "no healthy upstream")
				var he HTTPError
				require.ErrorAs(t, err, &he)
				require.Equal(t, http.StatusServiceUnavailable, he.StatusCode)
				require.LessOrEqual(t, time.Since(start), 3100*time.Millisecond)
			})
		})
	}
}

func TestRetryAfterMinimum(t *testing.T) {
	for _, header := range []string{"0", "5", "date"} {
		t.Run(header, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				start := time.Now()
				value := header
				if value == "date" {
					value = start.Add(5 * time.Second).Format(http.TimeFormat)
				}
				attempts := 0
				failedBody := &retryTestBody{Reader: bytes.NewBufferString("no healthy upstream")}
				c := &Cache{opt: Opt{
					Timeout:     time.Minute,
					BackoffPool: &BackoffPool{},
					Client: &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
						attempts++
						if attempts == 1 {
							return &http.Response{
								StatusCode: http.StatusServiceUnavailable,
								Header:     http.Header{"Retry-After": {value}},
								Body:       failedBody,
							}, nil
						}
						require.True(t, failedBody.closed)
						return &http.Response{StatusCode: http.StatusOK, Body: http.NoBody}, nil
					})},
				}}
				resp, err := c.doWithRetries(t.Context(), &request{
					method: http.MethodGet,
					url:    "https://cache.example/test",
				})
				require.NoError(t, err)
				resp.Body.Close()
				require.Equal(t, 2, attempts)
				if header == "0" {
					require.GreaterOrEqual(t, time.Since(start), minBackoff/2)
					require.Less(t, time.Since(start), minBackoff)
				} else {
					require.Equal(t, 5*time.Second, time.Since(start))
				}
			})
		})
	}
}

func TestRetryCancellation(t *testing.T) {
	for _, header := range []string{"", "30"} {
		t.Run("RetryAfter="+header, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				ctx, cancel := context.WithCancel(t.Context())
				defer cancel()
				pool := &BackoffPool{}
				defer pool.Reset()
				body := &retryTestBody{Reader: bytes.NewBufferString("unavailable")}
				attempts := 0
				c := &Cache{opt: Opt{
					Timeout:     time.Minute,
					BackoffPool: pool,
					Client: &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
						attempts++
						return &http.Response{
							StatusCode: http.StatusServiceUnavailable,
							Header:     http.Header{"Retry-After": {header}},
							Body:       body,
						}, nil
					})},
				}}
				done := make(chan error, 1)
				go func() {
					_, err := c.doWithRetries(ctx, &request{
						method: http.MethodGet,
						url:    "https://cache.example/test",
					})
					done <- err
				}()
				synctest.Wait()
				require.True(t, body.closed)
				start := time.Now()
				cancel()
				require.ErrorContains(t, <-done, "context canceled")
				require.Equal(t, 1, attempts)
				require.Zero(t, time.Since(start))
			})
		})
	}
}

type retryTestBody struct {
	io.Reader
	closed bool
}

func (b *retryTestBody) Close() error {
	b.closed = true
	return nil
}

func TestTokenScopes(t *testing.T) {
	// this token is expired
	noValidateToken = true
	defer func() {
		noValidateToken = false
	}()
	c, err := New("eyJ0eXAiOiJKV1QiLCJhbGciOiJSUzI1NiIsIng1dCI6InNiM29QbEhYRi0tV3lEZFBwM0FXRzEtWFhJdyJ9.eyJuYW1laWQiOiJkZGRkZGRkZC1kZGRkLWRkZGQtZGRkZC1kZGRkZGRkZGRkZGQiLCJzY3AiOiJBY3Rpb25zLkdlbmVyaWNSZWFkOjAwMDAwMDAwLTAwMDAtMDAwMC0wMDAwLTAwMDAwMDAwMDAwMCBBY3Rpb25zLlVwbG9hZEFydGlmYWN0czowMDAwMDAwMC0wMDAwLTAwMDAtMDAwMC0wMDAwMDAwMDAwMDAvMTpCdWlsZC9CdWlsZC83NiBMb2NhdGlvblNlcnZpY2UuQ29ubmVjdCBSZWFkQW5kVXBkYXRlQnVpbGRCeVVyaTowMDAwMDAwMC0wMDAwLTAwMDAtMDAwMC0wMDAwMDAwMDAwMDAvMTpCdWlsZC9CdWlsZC83NiIsIklkZW50aXR5VHlwZUNsYWltIjoiU3lzdGVtOlNlcnZpY2VJZGVudGl0eSIsImh0dHA6Ly9zY2hlbWFzLnhtbHNvYXAub3JnL3dzLzIwMDUvMDUvaWRlbnRpdHkvY2xhaW1zL3NpZCI6IkRERERERERELUREREQtRERERC1ERERELURERERERERERERERCIsImh0dHA6Ly9zY2hlbWFzLm1pY3Jvc29mdC5jb20vd3MvMjAwOC8wNi9pZGVudGl0eS9jbGFpbXMvcHJpbWFyeXNpZCI6ImRkZGRkZGRkLWRkZGQtZGRkZC1kZGRkLWRkZGRkZGRkZGRkZCIsImF1aSI6IjVkYzhiNDZmLWQzODctNDYxOS04MTM0LTgyNTAzM2I0NWM1MSIsInNpZCI6ImVjMTY4YTc5LWVmZTctNDc0OC05NjZjLTgwYTdkMjJmNTQ0NyIsImFjIjoiW3tcIlNjb3BlXCI6XCJyZWZzL2hlYWRzL3Rlc3RcIixcIlBlcm1pc3Npb25cIjozfSx7XCJTY29wZVwiOlwicmVmcy9oZWFkcy9tYXN0ZXJcIixcIlBlcm1pc3Npb25cIjoxfV0iLCJvcmNoaWQiOiIyNzEyOTAzZi01NzJjLTQxMjEtYmQwMC1kZDJhOTI0MDczMDIuaGVsbG9fd29ybGRfam9iLl9fZGVmYXVsdCIsImlzcyI6InZzdG9rZW4uYWN0aW9ucy5naXRodWJ1c2VyY29udGVudC5jb20iLCJhdWQiOiJ2c3Rva2VuLmFjdGlvbnMuZ2l0aHVidXNlcmNvbnRlbnQuY29tfHZzbzpmMTE5YzYyNS0yYzU1LTQ1MTgtYThmZC1jZGIyMzliYTNjMGYiLCJuYmYiOjE2MDY4MTI2MjksImV4cCI6MTYwNjgzNTQyOX0.lYlDkfZ6VHimS8Y5NdEmLdIqYwekB3pGBhtg6hLEb3s-Vdm6hLOP-8Ukmi0PWipSaFA33LqC5T-i1OSdM1eRCpcwZ0CES9ii4HcBrsE5JfoyGLHiYiUa5HvRJNDUd9Cbt0w_oghDV7fZ-kMOx7r4mfvaeUQDVS_fs9tCi6LFyG6h6ItYdddTsBfV9yPwjbyBSZIGTXiuaEhEYfJl24P9TRMjPUWYDeA0t_ERohowOlVCnHqJfOfrBtwEipsUN3OujLozYdoiPddhmzmer0D-HLo9VwGQllmyiaEF7MdVi7hjA44phULph62IWiTPbr-1ktOhLMTP1V-8CvF1nse59w", "", false, Opt{})
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
	key := newID()
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

	// v2 API is not immediately consistent
	time.Sleep(2 * time.Second)

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

	id := newID()
	err = c.Save(ctx, id, NewBlob([]byte("0123456789")))
	require.NoError(t, err)

	UploadChunkSize = oldChunkSize

	// v2 API is not immediately consistent
	time.Sleep(2 * time.Second)

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

	rand := newID()

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

	// v2 API is not immediately consistent
	time.Sleep(2 * time.Second)

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

	key := "mutable-" + newID()

	err = c.SaveMutable(ctx, key, 10*time.Second, func(ce *Entry) (Blob, error) {
		require.Nil(t, ce)
		return NewBlob([]byte("abc")), nil
	})
	require.NoError(t, err)

	// v2 API is not immediately consistent
	time.Sleep(2 * time.Second)

	err = c.SaveMutable(ctx, key, 10*time.Second, func(ce *Entry) (Blob, error) {
		require.NotNil(t, ce)
		expIdx := 1
		require.Equal(t, fmt.Sprintf("%s#%d", key, expIdx), ce.Key)
		buf := &bytes.Buffer{}
		err := ce.WriteTo(ctx, buf)
		require.NoError(t, err)
		return NewBlob(append(buf.Bytes(), []byte("def")...)), nil
	})
	require.NoError(t, err)

	// v2 API is not immediately consistent
	time.Sleep(2 * time.Second)

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

	key := "mutable-race-" + newID()

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

	// v2 API is not immediately consistent
	time.Sleep(2 * time.Second)

	count := 0
	err = c.SaveMutable(ctx, key, 10*time.Second, func(ce *Entry) (Blob, error) {
		require.NotNil(t, ce)
		buf := &bytes.Buffer{}
		err := ce.WriteTo(ctx, buf)
		require.NoError(t, err)
		expIdx := 1
		if count == 0 {
			require.Equal(t, fmt.Sprintf("%s#%d", key, expIdx), ce.Key)
			addAnother()
		} else {
			require.NotEqual(t, fmt.Sprintf("%s#%d", key, expIdx), ce.Key)
		}
		count++
		return NewBlob(append(buf.Bytes(), []byte("789")...)), nil
	})
	require.NoError(t, err)

	// v2 API is not immediately consistent
	time.Sleep(2 * time.Second)

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

	key := "mutable-race-" + newID()

	// reserve key but don't do anything, as if crashed
	idx := 1
	_, _, err = c.reserve(ctx, fmt.Sprintf("%s#%d", key, idx))
	require.NoError(t, err)

	count := 0
	err = c.SaveMutable(ctx, key, 3*time.Second, func(ce *Entry) (Blob, error) {
		require.Nil(t, ce)
		count++
		return NewBlob([]byte("123")), nil
	})
	require.NoError(t, err)

	require.True(t, count > 1)

	// v2 API is not immediately consistent
	time.Sleep(2 * time.Second)

	ce, err := c.Load(ctx, key)
	require.NoError(t, err)
	require.NotNil(t, ce)

	expIdx := 2
	require.Equal(t, fmt.Sprintf("%s#%d", key, expIdx), ce.Key)
}

func TestParseRetryAfter(t *testing.T) {
	t.Run("seconds", func(t *testing.T) {
		delay, ok := parseRetryAfter("5")
		require.True(t, ok)
		require.Equal(t, 5*time.Second, delay)
	})

	t.Run("negative-seconds", func(t *testing.T) {
		delay, ok := parseRetryAfter("-1")
		require.False(t, ok)
		require.Equal(t, time.Duration(0), delay)
	})

	t.Run("http-date", func(t *testing.T) {
		target := time.Now().Add(500 * time.Millisecond).UTC()
		delay, ok := parseRetryAfter(target.Format(http.TimeFormat))
		require.True(t, ok)
		require.GreaterOrEqual(t, delay, time.Duration(0))
		require.LessOrEqual(t, delay, 2*time.Second)
	})

	t.Run("invalid", func(t *testing.T) {
		delay, ok := parseRetryAfter("nope")
		require.False(t, ok)
		require.Equal(t, time.Duration(0), delay)
	})

	t.Run("empty", func(t *testing.T) {
		delay, ok := parseRetryAfter("  ")
		require.False(t, ok)
		require.Equal(t, time.Duration(0), delay)
	})
	for _, value := range []string{"9223372037", "18446744073709551616"} {
		t.Run(value, func(t *testing.T) {
			delay, ok := parseRetryAfter(value)
			require.True(t, ok)
			require.Equal(t, time.Duration(math.MaxInt64), delay)
		})
	}
}

func TestV2WriteErrorMessage(t *testing.T) {
	for _, test := range []struct {
		name     string
		method   string
		response string
		want     string
		write    func(*Cache) error
	}{
		{
			name:     "reserve with message",
			method:   "CreateCacheEntry",
			response: `{"ok":false,"message":"You have reached your configured budget, your cache is now read only to prevent additional charges."}`,
			want:     "failed to reserve cache: You have reached your configured budget, your cache is now read only to prevent additional charges.",
			write: func(cache *Cache) error {
				_, err := cache.reserveV2(t.Context(), "key")
				return err
			},
		},
		{
			name:     "reserve without message",
			method:   "CreateCacheEntry",
			response: `{"ok":false}`,
			want:     "failed to reserve cache",
			write: func(cache *Cache) error {
				_, err := cache.reserveV2(t.Context(), "key")
				return err
			},
		},
		{
			name:     "commit with message",
			method:   "FinalizeCacheEntryUpload",
			response: `{"ok":false,"message":"cache is read only"}`,
			want:     "failed to commit cache: cache is read only",
			write: func(cache *Cache) error {
				return cache.commitV2(t.Context(), "key", 1)
			},
		},
		{
			name:     "commit without message",
			method:   "FinalizeCacheEntryUpload",
			response: `{"ok":false}`,
			want:     "failed to commit cache",
			write: func(cache *Cache) error {
				return cache.commitV2(t.Context(), "key", 1)
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				require.Equal(t, "/twirp/github.actions.results.api.v1.CacheService/"+test.method, request.URL.Path)
				_, err := writer.Write([]byte(test.response))
				require.NoError(t, err)
			}))
			defer server.Close()

			cache := &Cache{
				URL:   server.URL,
				Token: &jwt.Token{},
				opt:   optsWithDefaults(Opt{Client: server.Client()}),
			}
			require.EqualError(t, test.write(cache), test.want)
		})
	}
}
