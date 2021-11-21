package provider

import (
	"context"
	"errors"
	"fmt"
	"os"
	"regexp"
	"strings"

	"github.com/Masterminds/semver/v3"
	"github.com/go-semantic-release/semantic-release/v2/pkg/provider"
	"github.com/go-semantic-release/semantic-release/v2/pkg/semrel"
	"github.com/google/go-github/v40/github"
	"golang.org/x/oauth2"
)

var PVERSION = "dev"

type GitHubRepository struct {
	owner  string
	repo   string
	client *github.Client
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
		gheUrl := fmt.Sprintf("https://%s/api/v3/", gheHost)
		rClient, err := github.NewEnterpriseClient(gheUrl, gheUrl, oauthClient)
		if err != nil {
			return err
		}
		repo.client = rClient
	} else {
		repo.client = github.NewClient(oauthClient)
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

func (repo *GitHubRepository) GetCommits(fromSha, toSha string) ([]*semrel.RawCommit, error) {
	allCommits := make([]*semrel.RawCommit, 0)
	opts := &github.CommitsListOptions{
		SHA:         toSha,
		ListOptions: github.ListOptions{PerPage: 100},
	}
	done := false
	for {
		commits, resp, err := repo.client.Repositories.ListCommits(context.Background(), repo.owner, repo.repo, opts)
		if err != nil {
			return nil, err
		}
		for _, commit := range commits {
			sha := commit.GetSHA()
			if sha == fromSha {
				done = true
				break
			}
			allCommits = append(allCommits, &semrel.RawCommit{
				SHA:        sha,
				RawMessage: commit.Commit.GetMessage(),
			})
		}
		if done || resp.NextPage == 0 {
			break
		}
		opts.Page = resp.NextPage
	}

	return allCommits, nil
}

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
	tag := fmt.Sprintf("v%s", release.NewVersion)
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
