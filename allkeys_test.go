package actionscache

import (
	"context"
	"os"
	"testing"

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

	api, err := NewRestAPI(ghRepo, ghToken, Opt{})
	require.NoError(t, err)

	k := "allkeys_test_" + newID()

	m, err := c.AllKeys(ctx, api, "allkeys_test_")
	require.NoError(t, err)

	_, ok = m[k]
	require.False(t, ok)

	err = c.Save(ctx, k, NewBlob([]byte("foobar")))
	require.NoError(t, err)

	m, err = c.AllKeys(ctx, api, "allkeys_test_")
	require.NoError(t, err)

	_, ok = m[k]
	require.True(t, ok)
}
