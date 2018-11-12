package pullrequest

import (
	"github.com/brinick/github/client"
	"github.com/brinick/github/object"
	"testing"
)

// ------------------------------------------------------------------
// Mock things
// ------------------------------------------------------------------

type MockPageIterator struct {
	ncalls int
}

func (i *MockPageIterator) Next() *client.Page {
	if i.ncalls < 1 {
		return &client.Page{
			URL: "blah",
		}
	}

	return nil
}

type MockPRGetter struct{}

func (m *MockPRGetter) Pulls(branch, state, author string) (*object.PullsIterator, error) {
	return object.PullsIterator(MockPageIterator(0), nil)
}

type MockRepo struct {
	Owner, Name string
}

// ------------------------------------------------------------------
// Test things
// ------------------------------------------------------------------

func TestGetPulls(t *testing.T) {
	object.PageIterator = MockPageIterator

	prs, err := getPulls(MockPRGetter(), "master")
}

func TestFetch(t *testing.T) {

}
