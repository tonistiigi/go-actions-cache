package actionscache

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestAllKeys(t *testing.T) {
	ctx := context.TODO()

	ghToken, ok := os.LookupEnv("GITHUB_TOKEN")
	if !ok || ghToken == "" {
		t.Log("GITHUB_TOKEN not set")
		t.SkipNow()
	}
	ghRepo, ok := os.LookupEnv("GITHUB_REPOSITORY")
	if !ok || ghRepo == "" {
		t.Log("GITHUB_REPOSITORY not set")
		t.SkipNow()
	}

	c, err := TryEnv(Opt{})
	require.NoError(t, err)
	if c == nil {
		t.SkipNow()
	}

	if !c.IsV2 {
		t.Skip("rest API is only enabled for v2 in this repo")
	}

	api, err := NewRestAPI(ghRepo, ghToken, Opt{})
	require.NoError(t, err)

	k := "allkeys_test_" + newID()

	m, err := c.AllKeys(ctx, api, "allkeys_test_")
	require.NoError(t, err)

	_, ok = m[k]
	require.False(t, ok)

	err = c.Save(ctx, k, NewBlob([]byte("foobar")))
	require.NoError(t, err)

	// v2 API is not immediately consistent
	time.Sleep(1 * time.Second)

	m, err = c.AllKeys(ctx, api, "allkeys_test_")
	require.NoError(t, err)

	_, ok = m[k]
	require.True(t, ok)
}
