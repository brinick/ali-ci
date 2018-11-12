package pullrequest

// pullrequest fetches the pull requests from a given
// ALICE repo and branch that match a given commit status context.

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/brinick/github/object"
	"github.com/brinick/logging"
)

// --------------------------------------------------------

// PR is a distillation of certain attributes
// about a given pull request, including commit status information.
type PR struct {
	number       int    // if we fill from a PR, we use the PR number
	name         string // if we process the main branch, we use the branch name
	sha          string
	packageName  string
	repoPath     string
	author       string
	CreatedAt    time.Time
	UpdatedAt    time.Time
	Status       statusInfo
	IsMainBranch bool
}

func (pr PR) RepoPath() string {
	return pr.repoPath
}

func (pr PR) Name() string {
	return pr.name
}

func (pr PR) Number() int {
	return pr.number
}

func (pr PR) SHA() string {
	return pr.sha
}

func (pr PR) PackageName() string {
	return pr.packageName
}

func (pr PR) Author() string {
	return pr.author
}

// Equal checks if two fetched pull requests are equal
func (pr PR) Equal(other PR) bool {
	return pr.sha == other.sha && pr.number == other.number
}

func (pr PR) IsBranch() bool {
	return len(pr.name) > 0
}

// IsReviewed indicates if this PullRequestStatus has been reviewed
func (pr PR) IsReviewed() bool {
	return pr.Status["reviewed"]
}

// IsTested indicates if this PullRequestStatus has been tested
func (pr PR) IsTested() bool {
	return pr.Status["tested"]
}

func (pr PR) Branch() (*object.RepoBranch, error) {
	return pr.Repo().Branch(pr.name)
}

// Commit returns the Github Commit instance with this pull request's SHA.
func (pr PR) Commit() (*object.RepoCommit, error) {
	return pr.Repo().Commit(pr.sha)
}

// Repo gets the Github Repository instance that contains this pull request
func (pr PR) Repo() *object.Repository {
	return object.NewRepoFromPath(pr.repoPath)
}

// TestFailed indicates if this PullRequestStatus failed testing
func (pr PR) TestFailed() bool {
	return !pr.Status["success"]
}

func (pr PR) String() string {
	info := []string{}
	for k, v := range pr.Status {
		info = append(info, fmt.Sprintf("%s => %t", k, v))
	}

	if !pr.IsMainBranch {
		return fmt.Sprintf(
			"[%d - %s - %s][%s][%s]",
			pr.number,
			pr.sha,
			pr.author,
			strings.Join(info, ", "),
			pr.CreatedAt,
		)
	}
	return fmt.Sprintf(
		"[%s - %s - %s][%s][%s]",
		pr.name,
		pr.sha,
		pr.author,
		info,
		pr.CreatedAt,
	)
}

// --------------------------------------------------------
// --------------------------------------------------------
// --------------------------------------------------------
type PRFetcher interface {
	Fetch() ([]*PR, error)
}

func NewFetcher(repoPath string) *prFetcher {
	return &prFetcher{
		repo:   getRepoObject(repoPath),
		branch: "master",
	}
}

type prFetcher struct {
	repo                     *object.Repository
	branch                   string
	trustedUsers             []string
	shouldTrustCollaborators bool
	showMainBranch           bool
}

func (f *prFetcher) SetBranch(branch string) {
	f.branch = branch
}

func (f *prFetcher) SetTrustedUsers(users []string) {
	f.trustedUsers = users
}

func (f *prFetcher) SetTrustCollaborators(value bool) {
	f.shouldTrustCollaborators = value
}

func (f *prFetcher) SetShowMainBranch(value bool) {
	f.showMainBranch = value
}

// Fetch returns a list of pointers to PullRequestStatus structs
// for the given repository and branch, filtering for the given
// checkName and status.  If either checkName or status are empty
// all values will be returned.
func (f *prFetcher) Fetch(checkName, status string) ([]*PR, error) {
	return f.FetchWithContext(
		context.TODO(),
		checkName,
		status,
	)
}

// FetchWithContext returns a list of pointers to PullRequestStatus structs
// for the given repository and branch, filtering for the given
// checkName and status. If either checkName or status are empty,
// all values will be returned. If the context is done, the method
// will bail early with a context done error.
func (f *prFetcher) FetchWithContext(
	ctx context.Context,
	checkName, status string,
) ([]*PR, error) {

	pulls, err := getPulls(ctx, f.repo, f.branch)
	if err != nil {
		return nil, err
	}

	logging.Info(
		"Processing fetched pull requests",
		logging.F("npulls", len(pulls)),
	)

	fetched := []*PR{}
	for _, pull := range pulls {
		p, err := processPull(ctx, pull, checkName, status)

		if err != nil {
			switch err {
			case context.Canceled, context.DeadlineExceeded:
				return nil, err
			}

			logging.Error(
				"Unable to process pull request, skipping",
				logging.F("err", err),
				logging.F("pr", p.number),
			)
			continue
		}

		// augment
		p.repoPath = f.repo.Path()
		p.packageName = f.repo.Name()

		isReviewed := p.Status["reviewed"]

		if isReviewed ||
			isTrustedUser(pull.Author.Login, f.trustedUsers) ||
			(f.shouldTrustCollaborators && f.repo.IsCollaborator(pull.Author.Login)) {
			p.Status["reviewed"] = true
			fetched = append(fetched, p)
		}
	}

	if f.showMainBranch {
		logging.Debug("Checking main branch")
		branch, err := f.repo.Branch(f.branch)
		if err != nil {
			return nil, err
		}

		p, err := processMainBranch(ctx, branch, checkName, status)
		if err != nil {
			switch err {
			case context.Canceled, context.DeadlineExceeded:
			default:
				logging.Error(
					"Unable to process pull request, skipping",
					logging.F("err", err),
					logging.F("pr", p.number),
				)
			}

			return nil, err
		}

		// augment
		p.repoPath = f.repo.Path()
		p.packageName = f.repo.Name()
		fetched = append(fetched, p)
	}

	return fetched, nil
}

func getRepoObject(repo string) *object.Repository {
	tokens := strings.Split(repo, "/")
	owner, name := tokens[0], tokens[1]
	return object.NewRepo(owner, name)
}

// --------------------------------------------------------
// getPulls fetches the list of open pull requests
// in the given Github repository and on the given branch
func getPulls(
	ctx context.Context,
	r *object.Repository,
	branchName string,
) ([]*object.PullRequest, error) {

	var (
		state = "open"
		pulls []*object.PullRequest
		pull  *object.PullRequest
	)

	prs, err := r.PullsWithContext(ctx, branchName, state)
	if err != nil {
		return nil, err
	}

	for prs.HasNextWithContext(ctx) {
		pull = prs.Item()
		pulls = append(pulls, pull)
	}

	return pulls, prs.Err
}

// --------------------------------------------------------
// isTrustedUser checks if the given author is in the list
// of users to be trusted
func isTrustedUser(author string, trustedUsers []string) bool {
	return contains(author, trustedUsers)
}

// --------------------------------------------------------

func getTrustedTeamID(orgName, trustedTeamName string) (int, error) {
	org, err := object.Organisation(orgName)
	if err != nil {
		return 0, err
	}

	var (
		teamID int
		team   *object.Team
	)

	teams, _ := org.Teams()

	for teams.HasNext() {
		team = teams.Item()
		if team.Name == trustedTeamName {
			teamID = team.ID
			return teamID, nil
		}
	}

	if team == nil {
		err = fmt.Errorf("%s: team not found", trustedTeamName)
	}

	return teamID, err
}

// --------------------------------------------------------
// contains is a helper function to check if a given string
// is in a list of strings
func contains(needle string, haystack []string) bool {
	for _, elem := range haystack {
		if elem == needle {
			return true
		}
	}
	return false
}

// --------------------------------------------------------

// getCommitStatusInfo loops over the statuses associated with
// the given commit, and returns a statusInfo. It bails early
// if the context is done.
func getCommitStatusInfo(
	ctx context.Context,
	commit *object.RepoCommit,
	checkName, status string,
) (statusInfo, error) {

	var (
		cs   *object.CommitStatus
		info = statusInfo{
			"reviewed": false,
			"tested":   false,
			"success":  false,
		}
	)

	if checkName == "" && status == "" {
		return info, nil
	}

	statuses, _ := commit.Statuses()

	// Loop is broken early if the context is cancelled/deadline exceeded
	for statuses.HasNextWithContext(ctx) {
		cs = statuses.Item()
		if cs.Context == checkName {
			info = statusInfo{
				"reviewed": true,
				"tested":   contains(cs.State, []string{"success", "error", "failure"}),
				"success":  contains(cs.State, []string{"success"}),
			}
			break
		}

		if cs.State == "success" {
			info["reviewed"] = true
		}
	}

	return info, statuses.Err
}

// --------------------------------------------------------
// processPull
func processPull(
	ctx context.Context,
	pr *object.PullRequest,
	checkName, status string,
) (*PR, error) {

	logging.Debug("Processing pull request", logging.F("id", pr.Number))

	var (
		c   *object.RepoCommit
		err error
	)

	if c, err = pr.HeadCommitWithContext(ctx); err != nil {
		logging.Error("Could not get Head commit")
		return nil, err
	}

	info, err := getCommitStatusInfo(ctx, c, checkName, status)
	return &PR{
		number:       pr.Number,
		sha:          c.SHA,
		CreatedAt:    pr.CreatedAt,
		UpdatedAt:    pr.UpdatedAt,
		Status:       info,
		author:       pr.Author.Login,
		IsMainBranch: false,
	}, err
}

// --------------------------------------------------------
// processMainBranch
func processMainBranch(
	ctx context.Context,
	b *object.RepoBranch,
	checkName, status string,
) (*PR, error) {

	info, err := getCommitStatusInfo(ctx, b.Head, checkName, status)
	// We consider main branches as always reviewed,
	// since they are already in the main repository.
	info["reviewed"] = true

	return &PR{
		name:         b.Name,
		sha:          b.Head.SHA,
		CreatedAt:    b.Head.Commit.Info.CreatedAt, // yikes :-(
		Status:       info,
		author:       b.Head.Author.Login,
		IsMainBranch: true,
	}, err
}

// statusInfo gives a PullRequestStatus' current testing state
type statusInfo map[string]bool
