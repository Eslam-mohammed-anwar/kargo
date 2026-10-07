package git

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// newTaggedRemote creates a bare remote whose main branch has many versions of
// a file, with a mix of annotated and lightweight tags.
func newTaggedRemote(t *testing.T, tmp string, allowFilter bool) string {
	t.Helper()
	remote := filepath.Join(tmp, "remote.git")
	runGit(t, tmp, "init", "--bare", "--initial-branch=main", remote)
	runGit(t, remote, "config", "uploadpack.allowFilter", fmt.Sprint(allowFilter))
	seed := filepath.Join(tmp, "seed")
	runGit(t, tmp, "clone", remote, seed)
	for i := 0; i < 20; i++ {
		f := filepath.Join(seed, "app.txt")
		require.NoError(t, os.WriteFile(f, []byte(strings.Repeat(fmt.Sprintf("v%d\n", i), 1000)), 0o600))
		runGit(t, seed, "add", "-A")
		runGit(t, seed, "commit", "-m", fmt.Sprintf("release %d", i))
		if i%2 == 0 {
			runGit(t, seed, "tag", "-a", fmt.Sprintf("pr-prod-deploy-%02d", i), "-m", fmt.Sprintf("tag %d", i))
		} else {
			runGit(t, seed, "tag", fmt.Sprintf("pr-prod-deploy-%02d", i))
		}
	}
	runGit(t, seed, "push", "origin", "HEAD:main", "--tags")
	return remote
}

func countBlobs(t *testing.T, dir string) int {
	t.Helper()
	out := runGit(t, dir, "cat-file", "--batch-all-objects", "--batch-check=%(objecttype)")
	return strings.Count(out, "blob")
}

// TestBloblessCloneTagDiscovery clones the way Warehouse git discovery does
// (single branch, blob:none) and checks that tag discovery returns exactly what
// a full clone returns, without the history's blobs.
func TestBloblessCloneTagDiscovery(t *testing.T) {
	tmp := t.TempDir()
	remote := newTaggedRemote(t, tmp, true)
	url := "file://" + remote

	blobless, err := Clone(url, nil, &CloneOptions{BaseDir: tmp, SingleBranch: true, Filter: FilterBlobless})
	require.NoError(t, err)
	defer blobless.Close()
	full, err := Clone(url, nil, &CloneOptions{BaseDir: tmp, SingleBranch: true})
	require.NoError(t, err)
	defer full.Close()

	require.Equal(t, "blob:none", runGit(t, blobless.Dir(), "config", "remote.origin.partialclonefilter"))
	// Only the checked out tip's blob, against all 20 versions in the full clone.
	require.Equal(t, 1, countBlobs(t, blobless.Dir()))
	require.Equal(t, 20, countBlobs(t, full.Dir()))

	bloblessTags, err := blobless.ListTags()
	require.NoError(t, err)
	fullTags, err := full.ListTags()
	require.NoError(t, err)
	require.Len(t, bloblessTags, 20)
	require.Equal(t, fullTags, bloblessTags)
	// Listing tags must not lazily fetch blobs from the promisor remote.
	require.Equal(t, 1, countBlobs(t, blobless.Dir()))

	bloblessCommits, err := blobless.ListCommits(5, 0)
	require.NoError(t, err)
	fullCommits, err := full.ListCommits(5, 0)
	require.NoError(t, err)
	require.Equal(t, fullCommits, bloblessCommits)
}

// TestBloblessCloneServerWithoutFilterSupport checks that a server which
// doesn't support partial clones still yields a usable, full clone.
func TestBloblessCloneServerWithoutFilterSupport(t *testing.T) {
	tmp := t.TempDir()
	remote := newTaggedRemote(t, tmp, false)

	repo, err := Clone("file://"+remote, nil, &CloneOptions{BaseDir: tmp, SingleBranch: true, Filter: FilterBlobless})
	require.NoError(t, err)
	defer repo.Close()

	require.Equal(t, 20, countBlobs(t, repo.Dir()))
	tags, err := repo.ListTags()
	require.NoError(t, err)
	require.Len(t, tags, 20)
}
