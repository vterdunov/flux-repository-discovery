// Package discovery evaluates repository filters without network access.
package discovery

type RepositoryID int64
type Repository struct {
	ID       RepositoryID
	Owner    string
	Name     string
	Topics   []string
	Archived bool
	Fork     bool
}
type Input struct {
	ID       string `json:"id"`
	FullName string `json:"fullName"`
	Owner    string `json:"owner"`
	Name     string `json:"name"`
}
type Result struct {
	Inputs                []Input  `json:"inputs"`
	UnmatchedRepositories []string `json:"unmatchedRepositories"`
}
