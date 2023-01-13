package provider

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/go-semantic-release/semantic-release/v2/pkg/provider"
	"github.com/go-semantic-release/semantic-release/v2/pkg/semrel"
	"github.com/google/go-github/v44/github"
	"github.com/stretchr/testify/require"
)

func TestNewGithubRepository(t *testing.T) {
	require := require.New(t)

	var repo *GitHubRepository
	repo = &GitHubRepository{}
	err := repo.Init(map[string]string{})
	require.EqualError(err, "github token missing")

	repo = &GitHubRepository{}
	err = repo.Init(map[string]string{
		"github_enterprise_host": "",
		"slug":                   "owner/test-repo",
		"token":                  "token",
	})
	require.NoError(err)

	repo = &GitHubRepository{}
	err = repo.Init(map[string]string{
		"github_enterprise_host": "github.enterprise",
		"slug":                   "owner/test-repo",
		"token":                  "token",
	})
	require.NoError(err)
	require.Equal("github.enterprise", repo.client.BaseURL.Host)
}

func createGithubCommit(sha, message string) *github.RepositoryCommit {
	return &github.RepositoryCommit{SHA: &sha, Commit: &github.Commit{Message: &message}}
}

var (
	commitType = "commit"
	tagType    = "tag"
)

func createGithubRef(ref, sha string) *github.Reference {
	return &github.Reference{Ref: &ref, Object: &github.GitObject{SHA: &sha, Type: &commitType}}
}

func createGithubRefWithTag(ref, sha string) *github.Reference {
	return &github.Reference{Ref: &ref, Object: &github.GitObject{SHA: &sha, Type: &tagType}}
}

var (
	githubRepoPrivate   = true
	githubDefaultBranch = "master"
	githubRepoName      = "test-repo"
	githubOwnerLogin    = "owner"
	githubRepo          = github.Repository{
		DefaultBranch: &githubDefaultBranch,
		Private:       &githubRepoPrivate,
		Owner: &github.User{
			Login: &githubOwnerLogin,
		},
		Name: &githubRepoName,
	}
	githubCommits = []*github.RepositoryCommit{
		createGithubCommit("abcd", "feat(app): new new feature"),
		createGithubCommit("1111", "feat: to"),
		createGithubCommit("abcd", "feat(app): new feature"),
		createGithubCommit("dcba", "Fix: bug"),
		createGithubCommit("cdba", "Initial commit"),
		createGithubCommit("efcd", "chore: break\nBREAKING CHANGE: breaks everything"),
		createGithubCommit("2222", "feat: from"),
		createGithubCommit("beef", "fix: test"),
	}
	githubTags = []*github.Reference{
		createGithubRef("refs/tags/test-tag", "deadbeef"),
		createGithubRef("refs/tags/v1.0.0", "beefdead"),
		createGithubRef("refs/tags/v2.0.0", "deadbeef"),
		createGithubRef("refs/tags/v2.1.0-beta", "beefdead"),
		createGithubRef("refs/tags/v3.0.0-beta.2", "deadbeef"),
		createGithubRef("refs/tags/v3.0.0-beta.1", "beefdead"),
		createGithubRef("refs/tags/2020.04.19", "deadbeef"),
		createGithubRefWithTag("refs/tags/v1.1.1", "12345678"),
	}
)

//nolint:errcheck
//gocyclo:ignore
func githubHandler(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("Authorization") != "Bearer token" {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if r.Method == http.MethodGet && r.URL.Path == "/repos/owner/test-repo" {
		json.NewEncoder(w).Encode(githubRepo)
		return
	}

	if r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/repos/owner/test-repo/compare/") {
		li := strings.LastIndex(r.URL.Path, "/")
		shaRange := strings.Split(r.URL.Path[li+1:], "...")
		fromSha := shaRange[0]
		toSha := shaRange[1]
		start := 0
		end := 0
		for i, commit := range githubCommits {
			if commit.GetSHA() == toSha {
				start = i
			} else if commit.GetSHA() == fromSha {
				end = i
			}
		}
		json.NewEncoder(w).Encode(github.CommitsComparison{Commits: githubCommits[start:end]})
		return
	}
	if r.Method == http.MethodGet && r.URL.Path == "/repos/owner/test-repo/commits" {
		toSha := r.URL.Query().Get("sha")
		skip := 0
		for i, commit := range githubCommits {
			if commit.GetSHA() == toSha {
				skip = i
				break
			}
		}
		json.NewEncoder(w).Encode(githubCommits[skip:])
		return
	}
	if r.Method == http.MethodGet && r.URL.Path == "/repos/owner/test-repo/git/matching-refs/tags" {
		json.NewEncoder(w).Encode(githubTags)
		return
	}
	if r.Method == http.MethodPost && r.URL.Path == "/repos/owner/test-repo/git/refs" {
		var data map[string]string
		json.NewDecoder(r.Body).Decode(&data)
		r.Body.Close()
		if data["sha"] != "deadbeef" || data["ref"] != "refs/tags/v2.0.0" {
			http.Error(w, "invalid sha or ref", http.StatusBadRequest)
			return
		}
		fmt.Fprint(w, "{}")
		return
	}
	if r.Method == http.MethodPost && r.URL.Path == "/repos/owner/test-repo/releases" {
		var data map[string]string
		json.NewDecoder(r.Body).Decode(&data)
		r.Body.Close()
		if data["tag_name"] != "v2.0.0" {
			http.Error(w, "invalid tag name", http.StatusBadRequest)
			return
		}
		fmt.Fprint(w, "{}")
		return
	}
	if r.Method == http.MethodGet && r.URL.Path == "/repos/owner/test-repo/git/tags/12345678" {
		sha := "deadbeef"
		json.NewEncoder(w).Encode(github.Tag{
			Object: &github.GitObject{SHA: &sha, Type: &commitType},
		})
		return
	}
	http.Error(w, "invalid route", http.StatusNotImplemented)
}

func getNewGithubTestRepo(t *testing.T) (*GitHubRepository, *httptest.Server) {
	repo := &GitHubRepository{}
	err := repo.Init(map[string]string{
		"slug":  "owner/test-repo",
		"token": "token",
	})
	require.NoError(t, err)
	ts := httptest.NewServer(http.HandlerFunc(githubHandler))
	repo.client.BaseURL, _ = url.Parse(ts.URL + "/")
	return repo, ts
}

func TestGithubGetInfo(t *testing.T) {
	repo, ts := getNewGithubTestRepo(t)
	defer ts.Close()
	repoInfo, err := repo.GetInfo()
	require.NoError(t, err)
	require.Equal(t, githubDefaultBranch, repoInfo.DefaultBranch)
	require.Equal(t, githubOwnerLogin, repoInfo.Owner)
	require.Equal(t, githubRepoName, repoInfo.Repo)
	require.True(t, repoInfo.Private)
}

func TestGithubGetCommits(t *testing.T) {
	repo, ts := getNewGithubTestRepo(t)
	defer ts.Close()
	commits, err := repo.GetCommits("2222", "1111")
	require.NoError(t, err)
	require.Len(t, commits, 5)

	for i, c := range commits {
		idxOff := i + 1
		require.Equal(t, c.SHA, githubCommits[idxOff].GetSHA())
		require.Equal(t, c.RawMessage, githubCommits[idxOff].Commit.GetMessage())
	}
}

func TestGithubGetCommitsWithCompare(t *testing.T) {
	repo, ts := getNewGithubTestRepo(t)
	defer ts.Close()
	repo.compareCommits = true
	commits, err := repo.GetCommits("2222", "1111")
	require.NoError(t, err)
	require.Len(t, commits, 5)

	for i, c := range commits {
		idxOff := i + 1
		require.Equal(t, c.SHA, githubCommits[idxOff].GetSHA())
		require.Equal(t, c.RawMessage, githubCommits[idxOff].Commit.GetMessage())
	}
}

func TestGithubGetReleases(t *testing.T) {
	repo, ts := getNewGithubTestRepo(t)
	defer ts.Close()

	testCases := []struct {
		vrange          string
		re              string
		expectedSHA     string
		expectedVersion string
	}{
		{"", "", "deadbeef", "2020.4.19"},
		{"", "^v[0-9]*", "deadbeef", "2.0.0"},
		{"2-beta", "", "deadbeef", "2.1.0-beta"},
		{"3-beta", "", "deadbeef", "3.0.0-beta.2"},
		{"4-beta", "", "deadbeef", "4.0.0-beta"},
	}

	for _, tc := range testCases {
		t.Run(fmt.Sprintf("VersionRange: %s, RE: %s", tc.vrange, tc.re), func(t *testing.T) {
			releases, err := repo.GetReleases(tc.re)
			require.NoError(t, err)
			release, err := semrel.GetLatestReleaseFromReleases(releases, tc.vrange)
			require.NoError(t, err)
			require.Equal(t, tc.expectedSHA, release.SHA)
			require.Equal(t, tc.expectedVersion, release.Version)
		})
	}
}

func TestGithubCreateRelease(t *testing.T) {
	repo, ts := getNewGithubTestRepo(t)
	defer ts.Close()
	err := repo.CreateRelease(&provider.CreateReleaseConfig{NewVersion: "2.0.0", SHA: "deadbeef"})
	require.NoError(t, err)
}
