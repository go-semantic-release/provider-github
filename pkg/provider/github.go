package provider

import (
	"context"
	"errors"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/Masterminds/semver/v3"
	"github.com/go-semantic-release/semantic-release/v2/pkg/provider"
	"github.com/go-semantic-release/semantic-release/v2/pkg/semrel"
	"github.com/google/go-github/v49/github"
	"golang.org/x/oauth2"
)

var PVERSION = "dev"

type GitHubRepository struct {
	owner           string
	repo            string
	stripVTagPrefix bool
	client          *github.Client
	compareCommits  bool
}

func (repo *GitHubRepository) Init(config map[string]string) error {
	gheHost := config["github_enterprise_host"]
	if gheHost == "" {
		gheHost = os.Getenv("GITHUB_ENTERPRISE_HOST")
	}
	slug := config["slug"]
	if slug == "" {
		slug = os.Getenv("GITHUB_REPOSITORY")
	}
	token := config["token"]
	if token == "" {
		token = os.Getenv("GITHUB_TOKEN")
	}
	if token == "" {
		token = os.Getenv("GH_TOKEN")
	}
	if token == "" {
		return errors.New("github token missing")
	}

	if !strings.Contains(slug, "/") {
		return errors.New("invalid slug")
	}
	split := strings.Split(slug, "/")
	repo.owner = split[0]
	repo.repo = split[1]

	oauthClient := oauth2.NewClient(context.Background(), oauth2.StaticTokenSource(&oauth2.Token{AccessToken: token}))
	if gheHost != "" {
		gheURL := fmt.Sprintf("https://%s/api/v3/", gheHost)
		rClient, err := github.NewEnterpriseClient(gheURL, gheURL, oauthClient)
		if err != nil {
			return err
		}
		repo.client = rClient
	} else {
		repo.client = github.NewClient(oauthClient)
	}

	if config["github_use_compare_commits"] == "true" {
		repo.compareCommits = true
	}

	var err error
	stripVTagPrefix := config["strip_v_tag_prefix"]
	repo.stripVTagPrefix, err = strconv.ParseBool(stripVTagPrefix)

	if stripVTagPrefix != "" && err != nil {
		return fmt.Errorf("failed to set property strip_v_tag_prefix: %w", err)
	}

	return nil
}

func (repo *GitHubRepository) GetInfo() (*provider.RepositoryInfo, error) {
	r, _, err := repo.client.Repositories.Get(context.Background(), repo.owner, repo.repo)
	if err != nil {
		return nil, err
	}
	return &provider.RepositoryInfo{
		Owner:         r.GetOwner().GetLogin(),
		Repo:          r.GetName(),
		DefaultBranch: r.GetDefaultBranch(),
		Private:       r.GetPrivate(),
	}, nil
}

func (repo *GitHubRepository) getCommitsFromGithub(compareCommits bool, fromSha, toSha string, opts *github.ListOptions) ([]*github.RepositoryCommit, *github.Response, error) {
	if !compareCommits {
		return repo.client.Repositories.ListCommits(context.Background(), repo.owner, repo.repo, &github.CommitsListOptions{
			SHA:         toSha,
			ListOptions: *opts,
		})
	}
	compCommits, resp, err := repo.client.Repositories.CompareCommits(context.Background(), repo.owner, repo.repo, fromSha, toSha, opts)
	if err != nil {
		return nil, nil, err
	}
	return compCommits.Commits, resp, nil
}

func (repo *GitHubRepository) GetCommits(fromSha, toSha string) ([]*semrel.RawCommit, error) {
	compareCommits := repo.compareCommits
	if compareCommits && fromSha == "" {
		// we want all commits for the first release, hence disable compareCommits
		compareCommits = false
	}
	allCommits := make([]*semrel.RawCommit, 0)
	opts := &github.ListOptions{PerPage: 100}
	done := false
	for {
		commits, resp, err := repo.getCommitsFromGithub(compareCommits, fromSha, toSha, opts)
		if err != nil {
			return nil, err
		}
		for _, commit := range commits {
			sha := commit.GetSHA()
			// compare commits already returns the relevant commits and no extra filtering is needed
			if !compareCommits && sha == fromSha {
				done = true
				break
			}
			allCommits = append(allCommits, &semrel.RawCommit{
				SHA:        sha,
				RawMessage: commit.Commit.GetMessage(),
				Annotations: map[string]string{
					"author_login":    commit.GetAuthor().GetLogin(),
					"author_name":     commit.Commit.GetAuthor().GetName(),
					"author_email":    commit.Commit.GetAuthor().GetEmail(),
					"author_date":     commit.Commit.GetAuthor().GetDate().Format(time.RFC3339),
					"committer_login": commit.GetCommitter().GetLogin(),
					"committer_name":  commit.Commit.GetCommitter().GetName(),
					"committer_email": commit.Commit.GetCommitter().GetEmail(),
					"committer_date":  commit.Commit.GetCommitter().GetDate().Format(time.RFC3339),
				},
			})
		}
		if done || resp.NextPage == 0 {
			break
		}
		opts.Page = resp.NextPage
	}
	return allCommits, nil
}

//gocyclo:ignore
func (repo *GitHubRepository) GetReleases(rawRe string) ([]*semrel.Release, error) {
	re := regexp.MustCompile(rawRe)
	allReleases := make([]*semrel.Release, 0)
	opts := &github.ReferenceListOptions{Ref: "tags", ListOptions: github.ListOptions{PerPage: 100}}
	for {
		refs, resp, err := repo.client.Git.ListMatchingRefs(context.Background(), repo.owner, repo.repo, opts)
		if resp != nil && resp.StatusCode == 404 {
			return allReleases, nil
		}
		if err != nil {
			return nil, err
		}
		for _, r := range refs {
			tag := strings.TrimPrefix(r.GetRef(), "refs/tags/")
			if rawRe != "" && !re.MatchString(tag) {
				continue
			}
			objType := r.Object.GetType()
			if objType != "commit" && objType != "tag" {
				continue
			}
			foundSha := r.Object.GetSHA()
			// resolve annotated tag
			if objType == "tag" {
				resTag, _, err := repo.client.Git.GetTag(context.Background(), repo.owner, repo.repo, foundSha)
				if err != nil {
					continue
				}
				if resTag.Object.GetType() != "commit" {
					continue
				}
				foundSha = resTag.Object.GetSHA()
			}
			version, err := semver.NewVersion(tag)
			if err != nil {
				continue
			}
			allReleases = append(allReleases, &semrel.Release{SHA: foundSha, Version: version.String()})
		}
		if resp.NextPage == 0 {
			break
		}
		opts.Page = resp.NextPage
	}

	return allReleases, nil
}

func (repo *GitHubRepository) CreateRelease(release *provider.CreateReleaseConfig) error {
	prefix := "v"
	if repo.stripVTagPrefix {
		prefix = ""
	}

	tag := prefix + release.NewVersion
	isPrerelease := release.Prerelease || semver.MustParse(release.NewVersion).Prerelease() != ""

	if release.Branch != release.SHA {
		ref := "refs/tags/" + tag
		tagOpts := &github.Reference{
			Ref:    &ref,
			Object: &github.GitObject{SHA: &release.SHA},
		}
		_, _, err := repo.client.Git.CreateRef(context.Background(), repo.owner, repo.repo, tagOpts)
		if err != nil {
			return err
		}
	}

	opts := &github.RepositoryRelease{
		TagName:         &tag,
		Name:            &tag,
		TargetCommitish: &release.Branch,
		Body:            &release.Changelog,
		Prerelease:      &isPrerelease,
	}
	_, _, err := repo.client.Repositories.CreateRelease(context.Background(), repo.owner, repo.repo, opts)
	return err
}

func (repo *GitHubRepository) Name() string {
	return "GitHub"
}

func (repo *GitHubRepository) Version() string {
	return PVERSION
}
