package provider

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/go-semantic-release/semantic-release/v2/pkg/provider"
	"github.com/go-semantic-release/semantic-release/v2/pkg/semrel"
	"github.com/google/go-github/v49/github"
	"github.com/stretchr/testify/require"
)

var validTags = map[string]bool{
	"v2.0.0": true,
	"2.0.0":  true,
}

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
		"strip_v_tag_prefix":     "true",
	})
	require.NoError(err)
	require.Equal("github.enterprise", repo.client.BaseURL.Host)
}

var (
	commitType = "commit"
	tagType    = "tag"
	testSHA    = "deadbeef"

	githubAuthorLogin = "author-login"
	githubAuthorName  = "author"
	githubAuthorEmail = "author@github.com"
	githubTimestamp   = time.Now()

	githubAuthor = &github.CommitAuthor{
		Name:  &githubAuthorName,
		Email: &githubAuthorEmail,
		Date:  &githubTimestamp,
	}
)

func createGithubCommit(sha, message string) *github.RepositoryCommit {
	return &github.RepositoryCommit{
		SHA:       &sha,
		Commit:    &github.Commit{Message: &message, Author: githubAuthor, Committer: githubAuthor},
		Author:    &github.User{Login: &githubAuthorLogin},
		Committer: &github.User{Login: &githubAuthorLogin},
	}
}

func createGithubRef(ref string) *github.Reference {
	return &github.Reference{Ref: &ref, Object: &github.GitObject{SHA: &testSHA, Type: &commitType}}
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
		createGithubRef("refs/tags/test-tag"),
		createGithubRef("refs/tags/v1.0.0"),
		createGithubRef("refs/tags/v2.0.0"),
		createGithubRef("refs/tags/v2.1.0-beta"),
		createGithubRef("refs/tags/v3.0.0-beta.2"),
		createGithubRef("refs/tags/v3.0.0-beta.1"),
		createGithubRef("refs/tags/2020.04.19"),
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
		if data["sha"] != testSHA || (data["ref"] != "refs/tags/v2.0.0" && data["ref"] != "refs/tags/2.0.0") {
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

		if _, ok := validTags[data["tag_name"]]; !ok {
			http.Error(w, "invalid tag name", http.StatusBadRequest)
			return
		}

		fmt.Fprint(w, "{}")
		return
	}
	if r.Method == http.MethodGet && r.URL.Path == "/repos/owner/test-repo/git/tags/12345678" {
		sha := testSHA
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
		require.Equal(t, c.Annotations["author_login"], githubCommits[idxOff].GetAuthor().GetLogin())
		require.Equal(t, c.Annotations["author_name"], githubCommits[idxOff].Commit.GetAuthor().GetName())
		require.Equal(t, c.Annotations["author_email"], githubCommits[idxOff].Commit.GetAuthor().GetEmail())
		require.Equal(t, c.Annotations["committer_login"], githubCommits[idxOff].GetCommitter().GetLogin())
		require.Equal(t, c.Annotations["committer_name"], githubCommits[idxOff].Commit.GetCommitter().GetName())
		require.Equal(t, c.Annotations["committer_email"], githubCommits[idxOff].Commit.GetCommitter().GetEmail())
		require.Equal(t, c.Annotations["author_date"], githubCommits[idxOff].Commit.GetAuthor().GetDate().Format(time.RFC3339))
		require.Equal(t, c.Annotations["committer_date"], githubCommits[idxOff].Commit.GetCommitter().GetDate().Format(time.RFC3339))
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
		{"", "", testSHA, "2020.4.19"},
		{"", "^v[0-9]*", testSHA, "2.0.0"},
		{"2-beta", "", testSHA, "2.1.0-beta"},
		{"3-beta", "", testSHA, "3.0.0-beta.2"},
		{"4-beta", "", testSHA, "4.0.0-beta"},
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
	err := repo.CreateRelease(&provider.CreateReleaseConfig{NewVersion: "2.0.0", SHA: testSHA})
	require.NoError(t, err)
}

func TestGitHubStripVTagRelease(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(githubHandler))
	defer ts.Close()

	repo := &GitHubRepository{}
	err := repo.Init(map[string]string{
		"slug":               "owner/test-repo",
		"token":              "token",
		"strip_v_tag_prefix": "true",
	})
	require.NoError(t, err)
	repo.client.BaseURL, _ = url.Parse(ts.URL + "/")

	err = repo.CreateRelease(&provider.CreateReleaseConfig{NewVersion: "2.0.0", SHA: testSHA})
	require.NoError(t, err)
}
